package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// HashVersion 은 audit chain hash 함수의 wire format version 입니다 (Phase 11.C).
//
// v1: canonicalMetaJSON 7 키 (action·actor·occurredAt·outcome·seq·target·tenantId).
//
//	v0.9.0 ~ v0.12.x 모든 entry 에 적용. Stage 11.C-3 chain transition marker
//	이전 entry 는 영구 v1 유지 (append-only · backward compat 엄격).
//
// v3: canonicalMetaJSON 9 키 (v1 7 키 + keyEpoch + leaderEpoch 알파벳순).
//
//	Stage 11.C-3 transition marker 이후 entry 에 적용. nil keyEpoch/leaderEpoch
//	는 omitempty 로 미직렬화 — v1 entry(epoch 미주입)에 대해서는 v3 hash 함수
//	결과가 v1 과 byte-identical (backward compat 안전망).
//
// v2 wire version 은 bundle export schema 전용 — hash 함수 자체는 v1 과 동일.
// 본 stage(11.C-2)는 hash 함수 인프라만 — 실 Append 분기는 Stage 11.C-3.
type HashVersion int

const (
	HashVersionV1 HashVersion = 1
	HashVersionV3 HashVersion = 3
)

// ComputeEntryHash는 v1 hash 함수로 다음 입력의 sha256을 계산합니다 (§10.4):
//
//	hash_i = sha256( prevHash[32] ‖ payloadDigest[32] ‖ canonicalMetaJSONv1 )
//
// canonicalMetaJSONv1은 알파벳순 키, 공백 없는 JSON으로 외부 검증 도구가
// 같은 입력으로 같은 결과를 재현할 수 있어야 합니다 (§10.6).
//
// v1 meta 필드: tenantId, seq, occurredAt(RFC3339Nano UTC), actor, action, target, outcome.
// (error는 hash에 포함하지 않음 — outcome으로 충분, error 텍스트 변경이 체인을 깨면 안 됨)
//
// 본 함수는 backward compat 보장 — Stage 11.C-3 chain transition marker 이전
// entry 의 hash 재계산에 영구 사용. 신규 v3 chain 분기는 ComputeEntryHashV3.
func ComputeEntryHash(prevHash, payloadDigest Hash, e Entry) (Hash, error) {
	return computeEntryHash(prevHash, payloadDigest, e, HashVersionV1)
}

// ComputeEntryHashV3 는 v3 hash 함수로 다음 입력의 sha256을 계산합니다 (Phase 11.C-2):
//
//	hash_i = sha256( prevHash[32] ‖ payloadDigest[32] ‖ canonicalMetaJSONv3 )
//
// v3 meta 필드 = v1 7 키 + keyEpoch + leaderEpoch 알파벳순 삽입 (총 9 키).
// nil keyEpoch/leaderEpoch 는 omitempty — v1 entry(epoch 미주입) 결과는
// v1 hash 와 byte-identical (의도된 backward compat).
//
// 본 함수는 Stage 11.C-3 chain transition marker 이후 entry hash 계산에 사용
// 예정. 본 stage(11.C-2) 에서는 인프라만 추가 — Append 경로는 v1 유지.
func ComputeEntryHashV3(prevHash, payloadDigest Hash, e Entry) (Hash, error) {
	return computeEntryHash(prevHash, payloadDigest, e, HashVersionV3)
}

// computeEntryHash 는 ComputeEntryHash/ComputeEntryHashV3 공통 구현 — version
// flag 로 canonicalMetaJSON 변종을 선택합니다. 다른 모든 입력(prev·digest·sha256
// 구성)은 v1 / v3 공통.
func computeEntryHash(prevHash, payloadDigest Hash, e Entry, version HashVersion) (Hash, error) {
	var (
		meta []byte
		err  error
	)
	switch version {
	case HashVersionV1:
		meta, err = canonicalMetaJSONv1(e)
	case HashVersionV3:
		meta, err = canonicalMetaJSONv3(e)
	default:
		return Hash{}, fmt.Errorf("audit: unsupported hash version %d", version)
	}
	if err != nil {
		return Hash{}, fmt.Errorf("audit: canonicalMetaJSON v%d: %w", version, err)
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

// canonicalMetaJSONv1 은 Entry 의 v1 meta 필드를 알파벳순 7 키 + 공백 없는 JSON 으로
// 직렬화합니다. encoding/json 의 Marshal 은 struct 필드를 정의 순서로 출력하므로,
// 필드명을 알파벳순으로 명시하는 익명 struct 를 사용합니다 (외부 도구 호환성을 위한
// 단순한 방식).
//
// 본 함수는 v0.9.0 ~ v0.12.x 모든 entry 의 hash 계산에 사용되었습니다 —
// backward compat 엄격, 변경 0.
func canonicalMetaJSONv1(e Entry) ([]byte, error) {
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

// canonicalMetaJSONv3 은 Entry 의 v3 meta 필드를 알파벳순 9 키 + 공백 없는 JSON 으로
// 직렬화합니다 (Phase 11.C-2).
//
// v3 = v1 7 키 + keyEpoch + leaderEpoch (알파벳순 위치):
//
//	action < actor < keyEpoch < leaderEpoch < occurredAt < outcome < seq < target < tenantId
//
// nil 처리: omitempty — Entry.KeyEpoch / Entry.LeaderEpoch 가 nil 이면 필드 자체를
// 직렬화 omit 합니다. 따라서 v1 entry(둘 다 nil)에 대해 v3 함수 결과는 v1 함수와
// byte-identical — Stage 11.C-3 transition marker 까지의 모든 v1 entry 가 v3 함수로도
// 재계산 가능 (backward compat 안전망).
//
// non-nil 인 경우: int64 그대로 직렬화. multi-region HA failover 이후 chain integrity
// 완전 cover (D-P11C-3 = 둘 다).
func canonicalMetaJSONv3(e Entry) ([]byte, error) {
	// 알파벳순 키: action, actor, keyEpoch, leaderEpoch, occurredAt, outcome, seq, target, tenantId.
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
		Action      string     `json:"action"`
		Actor       actorJSON  `json:"actor"`
		KeyEpoch    *int64     `json:"keyEpoch,omitempty"`
		LeaderEpoch *int64     `json:"leaderEpoch,omitempty"`
		OccurredAt  string     `json:"occurredAt"`
		Outcome     string     `json:"outcome"`
		Seq         int64      `json:"seq"`
		Target      targetJSON `json:"target"`
		TenantID    string     `json:"tenantId"`
	}

	m := metaJSON{
		Action: e.Action,
		Actor: actorJSON{
			ID:        e.Actor.ID,
			IP:        e.Actor.IP,
			Type:      string(e.Actor.Type),
			UserAgent: e.Actor.UserAgent,
		},
		KeyEpoch:    e.KeyEpoch,
		LeaderEpoch: e.LeaderEpoch,
		OccurredAt:  e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Outcome:     string(e.Outcome),
		Seq:         e.Seq,
		Target: targetJSON{
			ID:   e.Target.ID,
			Type: e.Target.Type,
		},
		TenantID: string(e.TenantID),
	}
	return json.Marshal(m)
}
