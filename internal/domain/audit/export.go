package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ExportEntryLine은 NDJSON 한 라인 직렬화 결과입니다.
//
// 외부 검증 도구는 이 라인을 파싱하고 ComputeEntryHash와 동일한 입력으로
// 해시를 재계산하여 stored hash와 비교합니다. 따라서 라인 형식·필드명은
// canonicalMetaJSON과 동일한 알파벳 순서를 따릅니다.
//
// hash·payloadDigest·prevHash는 hex 문자열로 직렬화 (BLOB → 텍스트 안전).
type ExportEntryLine struct {
	Action        string       `json:"action"`
	Actor         exportActor  `json:"actor"`
	Error         *ErrorInfo   `json:"error,omitempty"`
	Hash          string       `json:"hash"`
	OccurredAt    string       `json:"occurredAt"`
	Outcome       string       `json:"outcome"`
	PayloadDigest string       `json:"payloadDigest"`
	PrevHash      string       `json:"prevHash"`
	Seq           int64        `json:"seq"`
	Target        exportTarget `json:"target"`
	TenantID      string       `json:"tenantId"`
}

type exportActor struct {
	ID        string `json:"id"`
	IP        string `json:"ip,omitempty"`
	Type      string `json:"type"`
	UserAgent string `json:"userAgent,omitempty"`
}

type exportTarget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// ExportSignatureLine은 NDJSON 마지막 라인입니다.
// 키는 모두 underscore prefix로 entry 라인과 구분 (`_keyId`, `_publicKey` 등).
type ExportSignatureLine struct {
	From         int64  `json:"_from"`
	KeyID        string `json:"_keyId"`
	PublicKey    string `json:"_publicKey"`    // hex
	SignedDigest string `json:"_signedDigest"` // sha256(모든 entry 라인 concatenated) hex
	Signature    string `json:"_signature"`    // hex
	To           int64  `json:"_to"`
}

// MarshalEntryLine은 Entry를 NDJSON 라인으로 직렬화합니다 (개행 미포함).
func MarshalEntryLine(e Entry) ([]byte, error) {
	line := ExportEntryLine{
		Action: e.Action,
		Actor: exportActor{
			ID:        e.Actor.ID,
			IP:        e.Actor.IP,
			Type:      string(e.Actor.Type),
			UserAgent: e.Actor.UserAgent,
		},
		Error:         e.Error,
		Hash:          hex.EncodeToString(e.Hash[:]),
		OccurredAt:    e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Outcome:       string(e.Outcome),
		PayloadDigest: hex.EncodeToString(e.PayloadDigest[:]),
		PrevHash:      hex.EncodeToString(e.PrevHash[:]),
		Seq:           e.Seq,
		Target: exportTarget{
			ID:   e.Target.ID,
			Type: e.Target.Type,
		},
		TenantID: string(e.TenantID),
	}
	return json.Marshal(line)
}

// SignedDigest는 ExportSignatureLine.SignedDigest 계산을 캡슐화합니다.
// 입력은 NDJSON entry 라인들(개행 포함)의 byte stream — 외부 도구가 같은
// 방식으로 sha256을 다시 돌려 검증할 수 있도록 결정적.
func SignedDigest(entryLinesWithNewlines []byte) [32]byte {
	return sha256.Sum256(entryLinesWithNewlines)
}

// MarshalSignatureLine은 ExportSignatureLine을 NDJSON 한 줄(개행 미포함)으로 직렬화합니다.
func MarshalSignatureLine(sig ExportSignatureLine) ([]byte, error) {
	out, err := json.Marshal(sig)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal signature line: %w", err)
	}
	return out, nil
}
