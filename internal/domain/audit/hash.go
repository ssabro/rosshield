package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// ComputeEntryHash는 다음 입력으로 sha256을 계산합니다 (§10.4):
//
//	hash_i = sha256( prevHash[32] ‖ payloadDigest[32] ‖ canonicalMetaJSON )
//
// canonicalMetaJSON은 알파벳순 키, 공백 없는 JSON으로 외부 검증 도구가
// 같은 입력으로 같은 결과를 재현할 수 있어야 합니다 (§10.6).
//
// meta 필드: tenantId, seq, occurredAt(RFC3339Nano UTC), actor, action, target, outcome.
// (error는 hash에 포함하지 않음 — outcome으로 충분, error 텍스트 변경이 체인을 깨면 안 됨)
func ComputeEntryHash(prevHash, payloadDigest Hash, e Entry) (Hash, error) {
	meta, err := canonicalMetaJSON(e)
	if err != nil {
		return Hash{}, fmt.Errorf("audit: canonicalMetaJSON: %w", err)
	}

	h := sha256.New()
	h.Write(prevHash[:])
	h.Write(payloadDigest[:])
	h.Write(meta)

	var out Hash
	copy(out[:], h.Sum(nil))
	return out, nil
}

// ComputePayloadDigest는 payload bytes의 sha256을 반환합니다.
// payload가 nil/빈 슬라이스면 sha256("") 반환 — 빈 페이로드도 결정적.
func ComputePayloadDigest(payload []byte) Hash {
	sum := sha256.Sum256(payload)
	return Hash(sum)
}

// canonicalMetaJSON은 Entry의 meta 필드를 알파벳순 키 + 공백 없는 JSON으로 직렬화합니다.
// encoding/json의 Marshal은 struct 필드를 정의 순서로 출력하므로, 필드명을 알파벳순으로
// 명시하는 익명 struct를 사용합니다 (외부 도구 호환성을 위한 단순한 방식).
func canonicalMetaJSON(e Entry) ([]byte, error) {
	// 알파벳순 키: action, actor, occurredAt, outcome, seq, target, tenantId.
	type actorJSON struct {
		ID        string `json:"id"`
		IP        string `json:"ip,omitempty"`
		Type      string `json:"type"`
		UserAgent string `json:"userAgent,omitempty"`
	}
	type targetJSON struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	type metaJSON struct {
		Action     string     `json:"action"`
		Actor      actorJSON  `json:"actor"`
		OccurredAt string     `json:"occurredAt"`
		Outcome    string     `json:"outcome"`
		Seq        int64      `json:"seq"`
		Target     targetJSON `json:"target"`
		TenantID   string     `json:"tenantId"`
	}

	m := metaJSON{
		Action: e.Action,
		Actor: actorJSON{
			ID:        e.Actor.ID,
			IP:        e.Actor.IP,
			Type:      string(e.Actor.Type),
			UserAgent: e.Actor.UserAgent,
		},
		OccurredAt: e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Outcome:    string(e.Outcome),
		Seq:        e.Seq,
		Target: targetJSON{
			ID:   e.Target.ID,
			Type: e.Target.Type,
		},
		TenantID: string(e.TenantID),
	}
	return json.Marshal(m)
}
