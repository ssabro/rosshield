package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ExportEntryLine은 NDJSON 한 라인 직렬화 결과입니다.
//
// 외부 검증 도구는 이 라인을 파싱하고 ComputeEntryHash와 동일한 입력으로
// 해시를 재계산하여 stored hash와 비교합니다. 따라서 라인 형식·필드명은
// canonicalMetaJSON과 동일한 알파벳 순서를 따릅니다.
//
// hash·payloadDigest·prevHash는 hex 문자열로 직렬화 (BLOB → 텍스트 안전).
//
// KeyEpoch 는 Phase 10.D-5 v2 bundle 추가 필드 — Entry.KeyEpoch 가 nil 이면 미노출
// (v1 호환). v2 bundle 에서는 entry 별 활성 chain key epoch 를 외부 검증 도구가
// chainKeyEpochs map 으로 cross-reference 합니다.
type ExportEntryLine struct {
	Action        string       `json:"action"`
	Actor         exportActor  `json:"actor"`
	Error         *ErrorInfo   `json:"error,omitempty"`
	Hash          string       `json:"hash"`
	KeyEpoch      *int64       `json:"keyEpoch,omitempty"`
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
//
// BundleVersion · ChainKeyEpochs 는 Phase 10.D-5 v2 추가 필드. v1 호환:
// BundleVersion 이 빈 문자열 또는 부재이면 v1 — fg-verify 가 모든 entry 를 epoch=1
// 로 default 처리. v2 (`"v2"`) 이면 ChainKeyEpochs map 으로 epoch 별 public key
// lookup 후 검증.
type ExportSignatureLine struct {
	BundleVersion  string                `json:"_bundleVersion,omitempty"` // "v2" 면 ChainKeyEpochs 활성.
	ChainKeyEpochs []ExportChainKeyEpoch `json:"_chainKeyEpochs,omitempty"`
	From           int64                 `json:"_from"`
	KeyID          string                `json:"_keyId"`
	PublicKey      string                `json:"_publicKey"`    // hex
	SignedDigest   string                `json:"_signedDigest"` // sha256(모든 entry 라인 concatenated) hex
	Signature      string                `json:"_signature"`    // hex
	To             int64                 `json:"_to"`
}

// ExportChainKeyEpoch 는 v2 bundle 의 chainKeyEpochs[] 한 원소입니다.
//
// audit_chain_keys 테이블 snapshot 의 외부 와이어 직렬화 — 외부 감사인이 epoch 별
// public key 를 신뢰할 수 있도록 모든 활성·폐기 epoch 를 포함합니다.
//
// RevokedAt 은 nullable — 활성 epoch 이면 omit, 폐기되었으면 RFC3339Nano UTC.
type ExportChainKeyEpoch struct {
	CreatedAt    string `json:"createdAt"`
	Epoch        int64  `json:"epoch"`
	KeyID        string `json:"keyId"`
	PublicKeyHex string `json:"publicKeyHex"`
	RevokedAt    string `json:"revokedAt,omitempty"`
}

// MarshalEntryLine은 Entry를 NDJSON 라인으로 직렬화합니다 (개행 미포함).
//
// Entry.KeyEpoch 가 nil 이면 ExportEntryLine.KeyEpoch 도 nil → omitempty 로 미노출
// (v1 호환). 비-nil 이면 v2 bundle 의 entry-level epoch metadata.
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
		KeyEpoch:      e.KeyEpoch,
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

// UnmarshalEntryLine은 NDJSON 한 라인을 Entry로 역직렬화합니다 (MarshalEntryLine의 역).
//
// 외부 검증 도구 (rosshield-audit-verify rotation)는 entries.ndjson을 읽어 Entry를
// 복원한 뒤 ComputeSegmentHash로 segment hash를 재계산해 manifest 값과 비교합니다.
//
// hash·payloadDigest·prevHash는 hex 디코드 + 32 byte 길이 검증.
// 시간 필드는 RFC3339Nano.
func UnmarshalEntryLine(line []byte) (Entry, error) {
	var raw ExportEntryLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return Entry{}, fmt.Errorf("audit: unmarshal entry line: %w", err)
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, raw.OccurredAt)
	if err != nil {
		return Entry{}, fmt.Errorf("audit: parse occurredAt %q: %w", raw.OccurredAt, err)
	}

	payloadDigest, err := decodeHex32(raw.PayloadDigest, "payloadDigest")
	if err != nil {
		return Entry{}, err
	}
	prevHash, err := decodeHex32(raw.PrevHash, "prevHash")
	if err != nil {
		return Entry{}, err
	}
	hash, err := decodeHex32(raw.Hash, "hash")
	if err != nil {
		return Entry{}, err
	}

	e := Entry{
		TenantID:   storage.TenantID(raw.TenantID),
		Seq:        raw.Seq,
		OccurredAt: occurredAt,
		Actor: Actor{
			Type:      ActorType(raw.Actor.Type),
			ID:        raw.Actor.ID,
			IP:        raw.Actor.IP,
			UserAgent: raw.Actor.UserAgent,
		},
		Action:        raw.Action,
		Target:        Target{Type: raw.Target.Type, ID: raw.Target.ID},
		PayloadDigest: payloadDigest,
		Outcome:       Outcome(raw.Outcome),
		PrevHash:      prevHash,
		Hash:          hash,
		Error:         raw.Error,
		KeyEpoch:      raw.KeyEpoch,
	}
	return e, nil
}

func decodeHex32(s, field string) (Hash, error) {
	var h Hash
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("audit: decode %s hex: %w", field, err)
	}
	if len(b) != HashSize {
		return h, fmt.Errorf("audit: %s size = %d, want %d", field, len(b), HashSize)
	}
	copy(h[:], b)
	return h, nil
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

// BundleVersionV2 는 Phase 10.D-5 export bundle 의 _bundleVersion 마커입니다.
//
// v1 bundle 은 _bundleVersion 부재 → fg-verify 가 모든 entry 를 epoch=1 default 로 처리.
const BundleVersionV2 = "v2"

// ToExportChainKeyEpochs 는 도메인 ChainKeyEpoch slice 를 와이어 type 으로 변환합니다.
//
// 외부 감사인이 epoch 별 public key 를 cross-reference 할 수 있도록 v2 bundle 의
// signature line `_chainKeyEpochs` 필드에 직렬화됩니다.
func ToExportChainKeyEpochs(epochs []ChainKeyEpoch) []ExportChainKeyEpoch {
	if len(epochs) == 0 {
		return nil
	}
	out := make([]ExportChainKeyEpoch, 0, len(epochs))
	for _, e := range epochs {
		row := ExportChainKeyEpoch{
			CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339Nano),
			Epoch:        e.Epoch,
			KeyID:        e.KeyID,
			PublicKeyHex: e.PublicKeyHex,
		}
		if e.RevokedAt != nil {
			row.RevokedAt = e.RevokedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, row)
	}
	return out
}
