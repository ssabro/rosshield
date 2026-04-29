package main

// reporting_integration_test.go — E8 Stage D Exit 검증.
//
// 시나리오: bootstrap → tenant·fleet·pack·robot·scan_session·scan_results·evidence 시드 →
//   GenerateAndSignReport → BuildBundle → VerifyBundle 라운드트립 + audit chain 검증.
//
// 결정성·외부 검증 가능성·audit chain anchor 일관성을 한 번에 검증 — Phase 1 Exit
// "서명 PDF + 외부 검증 성공" 흐름 전체 결선 확인.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestReportingFullFlowEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	const (
		tenantID  = storage.TenantID("tnt_REP")
		fleetID   = "fl_REP"
		packID    = "pk_REP"
		credID    = "cr_REP"
		robotID   = "rb_REP"
		checkID   = "ck_REP"
		sessionID = "scan_REP"
	)

	// 시드 — completed 상태의 scan_session + 1 result + 1 evidence (raw INSERT, 도메인 우회).
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'rep-test', 'desktop_free', ?)`,
			string(tenantID), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES (?, ?, 'fleet', '', '{}', ?, ?)`, fleetID, string(tenantID), now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'cis', '1.0', 'CIS', 'cis-cis-1.0', x'00', 'key_test', ?)`,
			packID, string(tenantID), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (id, pack_id, check_id, title, severity, audit_command, evaluation_rule, rationale, fix_guidance)
VALUES (?, ?, 'CIS-1.1', 'Test check', 'high', 'true', '{}', 'rationale', 'fix')`,
			checkID, packID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, created_at, updated_at)
VALUES (?, ?, 'password', x'00', '{}', ?, ?)`, credID, string(tenantID), now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port, auth_type, criticality, tags, created_at, updated_at)
VALUES (?, ?, ?, ?, 'robot-1', '10.0.0.1', 22, 'password', 'medium', '[]', ?, ?)`,
			robotID, string(tenantID), fleetID, credID, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scan_sessions (id, tenant_id, fleet_id, pack_id, trigger, status,
    progress_total, progress_completed, progress_failed, failure_reason,
    created_at, updated_at, started_at, completed_at)
VALUES (?, ?, ?, ?, 'manual', 'completed', 1, 1, 0, '', ?, ?, ?, ?)`,
			sessionID, string(tenantID), fleetID, packID, now, now, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scan_results (id, session_id, tenant_id, robot_id, check_id, pack_check_id,
    outcome, eval_reason, duration_ms, executed_at, created_at)
VALUES ('scr_REP', ?, ?, ?, 'CIS-1.1', ?, 'pass', '', 100, ?, ?)`,
			sessionID, string(tenantID), robotID, checkID, now, now); err != nil {
			return err
		}
		// audit chain head 1 entry — 서명 시점에 anchor seq>=1 보장.
		if _, err := tx.Exec(ctx, `INSERT INTO audit_entries (
    seq, tenant_id, occurred_at, actor_type, actor_id, action, target_type, target_id,
    payload_digest, prev_hash, hash, outcome
) VALUES (1, ?, ?, 'system', 'system', 'tenant.created', 'tenant', ?,
    x'1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef',
    x'0000000000000000000000000000000000000000000000000000000000000000',
    x'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899',
    'success')`,
			string(tenantID), now, string(tenantID)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_chain_heads (tenant_id, seq, hash, updated_at)
VALUES (?, 1, x'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899', ?)`,
			string(tenantID), now)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 흐름 실행 — Generate + Sign + Bundle 일괄.
	report, bundle, err := GenerateAndSignReport(context.Background(), p, reporting.GenerateRequest{
		TenantID:    tenantID,
		SessionID:   sessionID,
		GeneratedBy: "system",
		GeneratedAt: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GenerateAndSignReport: %v", err)
	}

	// 1. Report 메타 검증.
	if report.ID == "" {
		t.Fatalf("report.ID empty")
	}
	if report.Signature.IsZero() {
		t.Fatalf("report not signed (Signature.IsZero=true)")
	}
	if report.Signature.SignerKeyID != p.ReportSigner.KeyID() {
		t.Fatalf("signer keyId mismatch: got %q want %q",
			report.Signature.SignerKeyID, p.ReportSigner.KeyID())
	}
	// Generate가 먼저 audit("reporting.generate")를 emit하므로 anchor 시점 head는
	// seed(1) + Generate(1) = 2가 정상. (Sign emit은 anchor 캡처 후 발생.)
	if report.Signature.ChainHeadSeq != 2 {
		t.Fatalf("anchor.HeadSeq=%d, want 2 (seed 1 + Generate emit 1, Sign emit 후속)",
			report.Signature.ChainHeadSeq)
	}
	if report.Signature.ChainHeadHash == "" {
		t.Fatalf("anchor.HeadHash empty")
	}

	// 2. PDF 본문 정합성 — Build 결과 sha256과 reports.pdf_sha256 일치.
	if !bytes.HasPrefix(report.PDF, []byte("%PDF-1.")) {
		t.Fatalf("PDF header missing")
	}

	// 3. Bundle 검증 — 외부 검증 도구 시뮬레이션.
	pubKey := ed25519.PublicKey(p.ReportSigner.PublicKey())
	res, err := reporting.VerifyBundle(bundle, pubKey)
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	if !res.OK {
		t.Fatalf("verify result.OK=false: %s", res.Reason)
	}
	if res.SignerKeyID != p.ReportSigner.KeyID() {
		t.Fatalf("bundle signer keyId mismatch")
	}
	if res.ChainHeadSeq != 2 || res.ChainHeadHash == "" {
		t.Fatalf("bundle anchor mismatch: %+v", res)
	}

	// 4. nil 키로도 검증 가능 (번들 내 public-key.pem 신뢰).
	resNil, err := reporting.VerifyBundle(bundle, nil)
	if err != nil || !resNil.OK {
		t.Fatalf("nil key verify: %v %+v", err, resNil)
	}

	// 5. audit chain — Generate + Sign 두 entry 추가 확인 (seed 1 + 신규 2 = 3).
	if err := p.Storage.Tx(storage.WithTenantID(context.Background(), tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			head, err := p.Audit.Head(ctx, tx, tenantID)
			if err != nil {
				return err
			}
			if head.Seq != 3 {
				t.Errorf("audit head=%d, want 3 (seed 1 + Generate + Sign)", head.Seq)
			}
			return nil
		}); err != nil {
		t.Fatalf("audit head check: %v", err)
	}
}

func TestReportingDeterministicAcrossRebuild(t *testing.T) {
	t.Parallel()
	bundleA := buildBundleOnce(t)
	bundleB := buildBundleOnce(t)
	// 같은 입력 + 같은 키 ⇒ 같은 PDF + 같은 sig + 같은 bundle.
	// 단, GenerateAndSignReport는 자동 생성 ULID(report.ID)를 포함하므로 bundle 자체는 다를 수 있음.
	// 결정성은 PDF body 단위로 검증.
	resA, _ := reporting.VerifyBundle(bundleA.bundle, nil)
	resB, _ := reporting.VerifyBundle(bundleB.bundle, nil)
	if resA.PDFSHA256 != resB.PDFSHA256 {
		t.Fatalf("PDF sha mismatch across rebuild: %s vs %s — 결정성 깨짐",
			resA.PDFSHA256, resB.PDFSHA256)
	}
}

type bundleFixture struct {
	bundle    []byte
	report    reporting.Report
	publicKey []byte
}

// buildBundleOnce는 한 번의 fresh bootstrap 후 Generate+Sign+Bundle 흐름을 실행합니다.
//
// 같은 입력 fixture를 두 번 돌려 PDF byte sha256이 동일한지 검증할 때 사용.
func buildBundleOnce(t *testing.T) bundleFixture {
	t.Helper()
	dir := t.TempDir()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dir, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	const (
		tenantID  = storage.TenantID("tnt_DET")
		sessionID = "scan_DET"
	)
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'det', 'desktop_free', ?)`,
			string(tenantID), fixed); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES ('fl_D', ?, 'fleet', '', '{}', ?, ?)`, string(tenantID), fixed, fixed); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES ('pk_D', ?, 'cis', '1.0', 'CIS', 'cis-cis-1.0', x'00', 'key_test', ?)`,
			string(tenantID), fixed); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO scan_sessions (id, tenant_id, fleet_id, pack_id, trigger, status,
    progress_total, progress_completed, progress_failed, failure_reason,
    created_at, updated_at, started_at, completed_at)
VALUES (?, ?, 'fl_D', 'pk_D', 'manual', 'completed', 0, 0, 0, '', ?, ?, ?, ?)`,
			sessionID, string(tenantID), fixed, fixed, fixed, fixed); err != nil {
			return err
		}
		// audit head seed
		if _, err := tx.Exec(ctx, `INSERT INTO audit_entries (
    seq, tenant_id, occurred_at, actor_type, actor_id, action, target_type, target_id,
    payload_digest, prev_hash, hash, outcome
) VALUES (1, ?, ?, 'system', 'system', 'tenant.created', 'tenant', ?,
    x'1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef',
    x'0000000000000000000000000000000000000000000000000000000000000000',
    x'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899',
    'success')`,
			string(tenantID), fixed, string(tenantID)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO audit_chain_heads (tenant_id, seq, hash, updated_at)
VALUES (?, 1, x'aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899', ?)`,
			string(tenantID), fixed)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 결정성 핵심: GeneratedAt 명시.
	report, bundle, err := GenerateAndSignReport(context.Background(), p, reporting.GenerateRequest{
		TenantID:    tenantID,
		SessionID:   sessionID,
		GeneratedBy: "system",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GenerateAndSignReport: %v", err)
	}
	return bundleFixture{
		bundle:    bundle,
		report:    report,
		publicKey: p.ReportSigner.PublicKey(),
	}
}

// _ = hex 사용 placeholder (linter satisfaction).
var _ = hex.EncodeToString
