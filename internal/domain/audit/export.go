package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ChainExporter 는 v1 + v2 + v3 audit chain bundle export 진입점입니다 (Phase 11.B-5 + 11.C-4).
//
// design doc `docs/design/notes/soc2-readiness-design.md` §7.5 (Stage 11.B-5) — 외부
// 감사인 access wizard 에서 사용할 audit log bundle 표면을 도메인 interface 로 분리.
// sqliterepo.Repo 가 자동 만족 (Export + ExportV2 + ExportV3 메서드 보유).
//
// 본 interface 는 기존 audit.Service interface 와 분리되어 있습니다 — Service 는 광범위하게
// 사용되는 공개 표면(Append/Head/Verify/WriteCheckpoint/LatestCheckpoint 등) 이므로
// export 메서드를 신규 추가하면 mock 다수 갱신이 필요. ChainExporter 는 export endpoint
// 핸들러 + audit_export wizard 만 의존하면 충분.
//
// fromSeq <= 0 → 1, toSeq <= 0 또는 toSeq < fromSeq → head.Seq 까지.
// v2 keyRepo 가 nil 이면 v1 bundle (byte-identical).
// v3 는 v2 super-set + entry-level LeaderEpoch + signature line `_hashVersionTransitionAt`.
type ChainExporter interface {
	// ExportV2 는 v2 bundle 을 내보냅니다 (chainKeyEpochs 포함).
	// keyRepo 가 nil 이면 v1 bundle 로 fallback (Export 와 byte-identical).
	ExportV2(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, sgn signer.Signer, keyRepo ChainKeyRepository) (io.ReadCloser, error)

	// ExportV3 는 Phase 11.C-4 v3 bundle 을 내보냅니다.
	// keyRepo 는 v2 와 동일 의미 — chainKeyEpochs lookup. nil 이어도 v3 bundle marker
	// 자체는 유효(`_bundleVersion="v3"`) — chainKeyEpochs 가 비어도 v3 wire 보장.
	ExportV3(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, sgn signer.Signer, keyRepo ChainKeyRepository) (io.ReadCloser, error)
}

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
//
// LeaderEpoch 는 Phase 11.C-4 v3 bundle 추가 필드 — Entry.LeaderEpoch 가 nil 이면 미노출
// (v1/v2 호환). v3 bundle 에서는 entry 별 leader fence token 을 외부 도구가 cross-reference
// 하며 v3 hash function 의 9 키 input 일부를 형성합니다.
type ExportEntryLine struct {
	Action        string       `json:"action"`
	Actor         exportActor  `json:"actor"`
	Error         *ErrorInfo   `json:"error,omitempty"`
	Hash          string       `json:"hash"`
	KeyEpoch      *int64       `json:"keyEpoch,omitempty"`
	LeaderEpoch   *int64       `json:"leaderEpoch,omitempty"`
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
//
// HashVersionTransitionAt 은 Phase 11.C-4 v3 추가 필드 — bundle 안에 transition marker
// entry 가 포함되면 그 seq 를 명시 (외부 fg-verify v3 가 v1/v3 경계 식별). bundle 범위
// (from~to) 안에 transition entry 가 없으면 0 (omitempty).
type ExportSignatureLine struct {
	BundleVersion           string                `json:"_bundleVersion,omitempty"` // "v2" 또는 "v3".
	ChainKeyEpochs          []ExportChainKeyEpoch `json:"_chainKeyEpochs,omitempty"`
	From                    int64                 `json:"_from"`
	HashVersionTransitionAt int64                 `json:"_hashVersionTransitionAt,omitempty"` // v3 transition entry seq (0 = 미포함)
	KeyID                   string                `json:"_keyId"`
	PublicKey               string                `json:"_publicKey"`    // hex
	SignedDigest            string                `json:"_signedDigest"` // sha256(모든 entry 라인 concatenated) hex
	Signature               string                `json:"_signature"`    // hex
	To                      int64                 `json:"_to"`
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
//
// LeaderEpoch 는 본 함수가 절대 노출하지 않습니다 (v1/v2 wire 호환 엄격) — v3 bundle
// 에서는 MarshalEntryLineV3 를 사용. HA 활성 환경에서 audit_entries.leader_epoch 가 채워져
// 있더라도 v1/v2 bundle 의 wire format 은 변경 0.
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

// MarshalEntryLineV3 는 Entry 를 v3 NDJSON 라인으로 직렬화합니다 (Phase 11.C-4).
//
// MarshalEntryLine 의 super-set — KeyEpoch + LeaderEpoch 둘 다 노출 (둘 다 nil 이면
// 자동 omit). v3 hash function 의 9 키 input 과 일관 — 외부 fg-verify v3 가 line 에서
// keyEpoch + leaderEpoch 를 읽어 v3 hash recompute.
//
// v2 bundle 에서는 MarshalEntryLine (v2 wire — leaderEpoch 미노출) 사용 — wire 호환 엄격.
func MarshalEntryLineV3(e Entry) ([]byte, error) {
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
		LeaderEpoch:   e.LeaderEpoch,
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
		LeaderEpoch:   raw.LeaderEpoch,
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

// BundleVersionV3 는 Phase 11.C-4 export bundle 의 _bundleVersion 마커입니다.
//
// v3 bundle 추가 보장:
//   - 각 entry line 이 LeaderEpoch (둘 다 nil 이면 omit) 노출.
//   - signature line 의 _hashVersionTransitionAt 가 bundle 범위 안 transition entry seq 노출 (없으면 omit).
//   - chain transition 이후 entry 는 v3 hash function (9 키 canonicalMetaJSON) 으로 hash 계산.
//
// 외부 fg-verify v3 binary 가 v1/v2/v3 자동 감지 — _bundleVersion 부재 → v1, "v2" → v2,
// "v3" → v3.
const BundleVersionV3 = "v3"

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
