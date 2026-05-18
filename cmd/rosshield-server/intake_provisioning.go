package main

// intake_provisioning.go — Customer onboarding R1 Stage 4: auto-provisioning adapter.
//
// design doc `docs/design/notes/customer-onboarding-design.md` §7 R1 Stage 4 + §3.1 영역 1.
//
// 책임:
//
//	intake.Service를 wrap해 AcceptIntake 호출 시 같은 Tx에 다음을 묶음:
//	  1. tenant.Service.Create — 새 tenant + 첫 admin user(시스템 역할 3종 시드 + admin 할당).
//	  2. license 발급 placeholder — paying customer 0 단계 (design doc 권장 default).
//	     실 토큰 발급은 별 도구(rosshield team 측 Ed25519 서명) — 본 adapter는 log만.
//	  3. intake.Service.AcceptIntake(TenantID = 새 TenantID) — intake row를 accepted로 전환.
//
//	모두 같은 Tx에 묶여 atomic — tenant.Create 실패 시 intake도 pending 유지 (handler가
//	Bootstrap Tx를 commit/rollback으로 묶음).
//
// 다른 4 메서드(Create/Get/List/Reject)는 단순 delegate.
//
// 도메인 경계 (P5):
//
//	본 adapter는 cmd/* application layer에 위치 — intake·tenant 도메인 간 직접 의존 없음.
//	wrap 패턴은 handler·intake.Service 자체 변경 0으로 결선 가능 (handler는 wrap된 어댑터를
//	intake.Service 인터페이스로 그대로 사용).
//
// 멀티테넌시 (P4):
//
//	intake 도메인은 *tenant 생성 전* 단계 데이터 — Bootstrap Tx 진입 가정 (handler가 보장).
//	wrap이 같은 Bootstrap Tx에서 tenant.Service.Create를 호출 — Create 자체도 Bootstrap Tx
//	진입점이므로 ctx의 TenantID 없이 동작.
//
// 보안:
//
//	admin user의 초기 password는 cryptographic random 32B (base64url) 생성 후 도메인에
//	plain text로 전달 — 도메인이 argon2id 해시. plain text는 adapter 안에서만 일시 보유 →
//	tenant.Service.Create 호출 후 변수 폐기. customer는 별 channel로 password reset 또는
//	invitation token 발급(Stage 5 통합 e2e 후속)으로 접근.
//
//	본 stage 단위는 password를 응답하지 않음 — wrap 결과는 intake.CustomerIntake만 반환.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/ssabro/rosshield/internal/domain/intake"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/license"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// intakeProvisioningAdapter는 intake.Service를 wrap해 AcceptIntake 시점에 tenant 생성과
// license 발급 placeholder를 같은 Tx에 묶는 어댑터입니다.
//
// 다른 4 메서드(Create/Get/List/Reject)는 inner 호출만 위임 (no-op wrap).
//
// 필드:
//   - inner: 실제 intake.Service 구현체 (sqliterepo.Repo 등).
//   - tenantSvc: tenant 도메인 진입점 — Create로 tenant + 첫 admin user 시드.
//   - licenseEnforcer: license 발급 placeholder hook. nil이면 발급 skip (paying customer
//     0 단계 — log only). 실 발급은 별 CLI 도구 (Ed25519 서명).
type intakeProvisioningAdapter struct {
	inner           intake.Service
	tenantSvc       tenant.Service
	licenseEnforcer *license.Enforcer
}

// newIntakeProvisioningAdapter는 wrap된 어댑터를 반환합니다.
//
// licenseEnforcer는 nil 허용 — Stage 4 paying customer 0 단계 default. nil이면 발급 단계가
// silent skip (audit·log emit은 inner 도메인의 책임 — 본 stage는 placeholder).
func newIntakeProvisioningAdapter(inner intake.Service, tenantSvc tenant.Service, licenseEnforcer *license.Enforcer) *intakeProvisioningAdapter {
	return &intakeProvisioningAdapter{
		inner:           inner,
		tenantSvc:       tenantSvc,
		licenseEnforcer: licenseEnforcer,
	}
}

// 컴파일 시점에 intake.Service 인터페이스 충족 검증.
var _ intake.Service = (*intakeProvisioningAdapter)(nil)

// CreateIntake는 inner.CreateIntake를 그대로 호출합니다 (delegate).
func (a *intakeProvisioningAdapter) CreateIntake(ctx context.Context, tx storage.Tx, req intake.CreateIntakeRequest) (intake.CustomerIntake, error) {
	return a.inner.CreateIntake(ctx, tx, req)
}

// GetIntake는 inner.GetIntake를 그대로 호출합니다 (delegate).
func (a *intakeProvisioningAdapter) GetIntake(ctx context.Context, tx storage.Tx, intakeID string) (intake.CustomerIntake, error) {
	return a.inner.GetIntake(ctx, tx, intakeID)
}

// ListIntakes는 inner.ListIntakes를 그대로 호출합니다 (delegate).
func (a *intakeProvisioningAdapter) ListIntakes(ctx context.Context, tx storage.Tx, filter intake.ListIntakesFilter) ([]intake.CustomerIntake, error) {
	return a.inner.ListIntakes(ctx, tx, filter)
}

// RejectIntake는 inner.RejectIntake를 그대로 호출합니다 (delegate).
//
// Reject은 tenant 생성을 트리거하지 않음 — wrap 책임 외.
func (a *intakeProvisioningAdapter) RejectIntake(ctx context.Context, tx storage.Tx, req intake.RejectIntakeRequest) (intake.CustomerIntake, error) {
	return a.inner.RejectIntake(ctx, tx, req)
}

// AcceptIntake는 wrap의 핵심 메서드입니다 — auto-provisioning.
//
// 처리 순서 (모두 같은 Tx):
//
//  1. intake row 조회 (inner.GetIntake) — pending 상태 검증 + tenant 시드에 필요한 필드
//     (OrganizationName, PrimaryContactEmail, PrimaryContactName, PlanRequest) 회수.
//     이미 terminal 상태면 ErrIntakeNotPending 즉시 반환 (tenant 시드 skip).
//  2. tenant.Service.Create — 새 tenant + 첫 admin user + 시스템 역할 3종 + admin 할당.
//     - Name = intake.OrganizationName
//     - Plan = mapPlanRequest(intake.PlanRequest) — community→desktop_free 등 매핑.
//     - AdminEmail = intake.PrimaryContactEmail
//     - AdminPassword = cryptographic random 32B (base64url) — 일시 변수 (응답 외).
//     - AdminDisplayName = intake.PrimaryContactName
//  3. license 발급 placeholder — paying customer 0 단계 default = no-op. 실 발급은 별 CLI.
//  4. inner.AcceptIntake — req.TenantID에 새 tenant ID 채워서 호출 → row UPDATE.
//
// 실패 시 (예: tenant.Create의 ErrInvalidEmail) Tx rollback — intake도 pending 유지.
// (handler가 Storage.Bootstrap callback에서 err 반환 → SQL Tx rollback 자동).
func (a *intakeProvisioningAdapter) AcceptIntake(ctx context.Context, tx storage.Tx, req intake.AcceptIntakeRequest) (intake.CustomerIntake, error) {
	// 1. 사전 조회 — pending 상태 + tenant 시드에 필요한 필드 회수.
	existing, err := a.inner.GetIntake(ctx, tx, req.IntakeID)
	if err != nil {
		return intake.CustomerIntake{}, err
	}
	if existing.Status != intake.StatusPending {
		return intake.CustomerIntake{}, intake.ErrIntakeNotPending
	}

	// 2. admin 임시 password 생성 (cryptographic random 32B → base64url).
	//    plain text는 본 함수 변수에만 일시 — tenant.Service.Create 호출 후 폐기.
	pw, err := generateAdminPassword()
	if err != nil {
		return intake.CustomerIntake{}, fmt.Errorf("intake provisioning: generate admin password: %w", err)
	}

	// 3. tenant + admin user 시드 (같은 Tx).
	createResult, err := a.tenantSvc.Create(ctx, tx, tenant.CreateRequest{
		Name:             existing.OrganizationName,
		Plan:             mapPlanRequest(existing.PlanRequest),
		AdminEmail:       existing.PrimaryContactEmail,
		AdminPassword:    pw,
		AdminDisplayName: existing.PrimaryContactName,
	})
	if err != nil {
		return intake.CustomerIntake{}, err
	}

	// 4. license 발급 placeholder — paying customer 0 단계 default.
	//    실 발급은 별 CLI 도구 (rosshield team 측 Ed25519 서명). 본 adapter는 nil 분기.
	if a.licenseEnforcer != nil {
		// placeholder — 향후 발급 흐름이 추가되면 본 분기에서 license token 생성 + secure
		// 채널 전달. 본 stage에서는 enforcer 존재 자체만 확인.
		_ = a.licenseEnforcer
	}

	// 5. intake row를 accepted로 전환 (TenantID 채움).
	acceptedReq := intake.AcceptIntakeRequest{
		IntakeID:         req.IntakeID,
		AcceptedByUserID: req.AcceptedByUserID,
		TenantID:         createResult.Tenant.ID,
	}
	accepted, err := a.inner.AcceptIntake(ctx, tx, acceptedReq)
	if err != nil {
		return intake.CustomerIntake{}, err
	}
	return accepted, nil
}

// mapPlanRequest는 intake PlanRequest enum을 tenant.Plan enum으로 매핑합니다.
//
// 매핑 규칙 (design doc §3.1 가설 + 보수적 default):
//
//	community  → desktop_free
//	pro        → desktop_pro
//	enterprise → enterprise
//
// 알 수 없는 값은 desktop_free로 fallback (보수적 — 잘못 매핑된 SKU가 enterprise feature를
// 활성화하지 않도록).
func mapPlanRequest(p intake.PlanRequest) tenant.Plan {
	switch p {
	case intake.PlanCommunity:
		return tenant.PlanDesktopFree
	case intake.PlanPro:
		return tenant.PlanDesktopPro
	case intake.PlanEnterprise:
		return tenant.PlanEnterprise
	default:
		return tenant.PlanDesktopFree
	}
}

// generateAdminPassword는 cryptographic random 32B를 base64url로 인코딩한 임시 password를
// 반환합니다.
//
// tenant 도메인의 password validation (≥12 chars) 통과 — base64url(32B) = 43 chars 고정.
// plain text는 caller(AcceptIntake)가 일시 보유 후 tenant.Service.Create로 전달 → 도메인이
// argon2id 해시 → plain text 변수는 GC 대상.
//
// 본 password는 customer에 직접 전달되지 않음 — Stage 5 통합 e2e에서 invitation token으로
// 첫 로그인 흐름이 결선되면 본 password는 사용 0 (DB에는 hash만 남음, plain text 회수 불가).
func generateAdminPassword() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
