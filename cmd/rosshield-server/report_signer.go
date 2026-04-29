package main

// report_signer.go — reporting.Generate → audit head 조회 → Sign → BuildBundle 일괄 처리 (E8 Stage D).
//
// 이 흐름은 application logic이라 도메인이 직접 호출하지 않고 cmd/* bootstrap이 담당합니다 —
// scanrun 패턴과 동일(`scanexec.go` adapter 결선).
//
// 한 Tx에서 처리:
//   1. Generate: PDF builder 실행 → reports INSERT (서명 placeholder)
//   2. audit.Head 조회: 서명 시점 chain head snapshot
//   3. ReportSigner.Sign(pdfBytes) → sigBytes + keyID
//   4. Service.Sign: sig_* 컬럼 UPDATE + audit emit("reporting.sign")
//   5. BuildBundle: 외부 검증 가능 tar.gz
//
// 결과: Report (Sign 완료) + tar.gz bundle bytes.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// GenerateAndSignReport는 ScanSession 1건의 결과를 PDF로 생성·서명·번들링합니다.
//
// 호출자(API handler·CLI·통합 테스트)는 결과 bundle bytes를 디스크에 쓰거나 응답으로 반환합니다.
// 실패 시 Tx rollback — 부분 row 잔여 없음.
func GenerateAndSignReport(ctx context.Context, p *Platform, req reporting.GenerateRequest) (reporting.Report, []byte, error) {
	if p == nil {
		return reporting.Report{}, nil, fmt.Errorf("reporting: platform nil")
	}

	var signed reporting.Report

	tenantCtx := storage.WithTenantID(ctx, req.TenantID)
	if err := p.Storage.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		// 1. Generate — PDF builder 실행 후 reports INSERT.
		r, err := p.Reporting.Generate(c, tx, req)
		if err != nil {
			return fmt.Errorf("generate: %w", err)
		}

		// 2. ReportSigner.Sign(pdfBytes) — 외부 ed25519 서명.
		// pdfBytes는 Generate가 반환한 r.PDF.
		sigBytes, signerKeyID, err := p.ReportSigner.Sign(r.PDF)
		if err != nil {
			return fmt.Errorf("signer: %w", err)
		}

		// 3. audit.Head — 서명 시점 chain head snapshot.
		head, err := p.Audit.Head(c, tx, req.TenantID)
		if err != nil {
			return fmt.Errorf("audit head: %w", err)
		}

		// 4. reporting.Service.Sign — sig_* UPDATE + audit emit.
		s, err := p.Reporting.Sign(c, tx, r.ID, signerKeyID, sigBytes,
			head.Seq, hex.EncodeToString(head.Hash[:]), p.Clock.Now())
		if err != nil {
			return fmt.Errorf("reporting sign: %w", err)
		}
		signed = s
		return nil
	}); err != nil {
		return reporting.Report{}, nil, err
	}

	// 5. BuildBundle — Tx 밖에서(파일 I/O 없음, pure 메모리).
	pubKey := ed25519.PublicKey(p.ReportSigner.PublicKey())
	bundle, err := reporting.BuildBundle(signed, pubKey)
	if err != nil {
		return reporting.Report{}, nil, fmt.Errorf("bundle: %w", err)
	}
	return signed, bundle, nil
}

// 컴파일 타임 가드: audit.ChainHead가 본 파일이 가정하는 표면을 가지고 있는지.
var _ = audit.ChainHead{}.Seq
