//go:build rosshield_enterprise

package robotid

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// TestCompute_결정론_같은_입력_같은_fingerprint
//
// design doc §6.5 알고리즘 1: 같은 Inputs → 같은 Fingerprint. 외부 검증자가
// 같은 입력으로 같은 fingerprint를 재현할 수 있어야 (audit 결정론).
func TestCompute_결정론_같은_입력_같은_fingerprint(t *testing.T) {
	in := Inputs{
		EKCert:    []byte{0x01, 0x02, 0x03},
		MACs:      []string{"aa:bb:cc:dd:ee:ff", "00:11:22:33:44:55"},
		CPUSerial: "ABCD1234",
		Salt:      []byte("tenant-salt-fixed"),
	}

	fp1, err := Compute(in)
	if err != nil {
		t.Fatalf("Compute first: %v", err)
	}
	fp2, err := Compute(in)
	if err != nil {
		t.Fatalf("Compute second: %v", err)
	}

	if fp1.Hash != fp2.Hash {
		t.Errorf("hash 결정론 위반: %s vs %s", fp1.Hash, fp2.Hash)
	}
	if fp1.Length != fp2.Length {
		t.Errorf("length mismatch: %d vs %d", fp1.Length, fp2.Length)
	}
	if fp1.HasTPM != fp2.HasTPM {
		t.Errorf("HasTPM mismatch")
	}
	if fp1.MACCount != fp2.MACCount {
		t.Errorf("MACCount mismatch")
	}
}

// TestCompute_MAC_정렬_invariant
//
// design doc §6.5 알고리즘: sorted_macs 결합. 입력 MAC 순서를 바꿔도
// 같은 fingerprint가 나와야 (정렬 invariant).
func TestCompute_MAC_정렬_invariant(t *testing.T) {
	salt := []byte("tenant-salt")
	a := Inputs{MACs: []string{"a1:b2:c3:d4:e5:f6", "11:22:33:44:55:66"}, Salt: salt}
	b := Inputs{MACs: []string{"11:22:33:44:55:66", "a1:b2:c3:d4:e5:f6"}, Salt: salt}

	fpA, err := Compute(a)
	if err != nil {
		t.Fatalf("Compute a: %v", err)
	}
	fpB, err := Compute(b)
	if err != nil {
		t.Fatalf("Compute b: %v", err)
	}

	if fpA.Hash != fpB.Hash {
		t.Errorf("MAC 순서 invariant 위반: %s vs %s", fpA.Hash, fpB.Hash)
	}
}

// TestCompute_salt_cross_tenant_분리
//
// design doc §6.5 알고리즘 3: salt는 tenant-level 고정. 같은 robot
// inputs이라도 다른 tenant salt → 다른 fingerprint → cross-tenant 누출 방지.
func TestCompute_salt_cross_tenant_분리(t *testing.T) {
	base := Inputs{
		EKCert:    []byte{0xAA, 0xBB},
		MACs:      []string{"de:ad:be:ef:00:01"},
		CPUSerial: "robot-1",
	}
	tenantA := base
	tenantA.Salt = []byte("tenant-A-salt")
	tenantB := base
	tenantB.Salt = []byte("tenant-B-salt")

	fpA, err := Compute(tenantA)
	if err != nil {
		t.Fatalf("Compute tenantA: %v", err)
	}
	fpB, err := Compute(tenantB)
	if err != nil {
		t.Fatalf("Compute tenantB: %v", err)
	}

	if fpA.Hash == fpB.Hash {
		t.Errorf("salt cross-tenant 분리 실패: %s == %s", fpA.Hash, fpB.Hash)
	}
}

// TestCompute_empty_fields_OK
//
// nil EKCert / 빈 MACs / 빈 CPUSerial 모두 허용 — TPM 없는 로봇, MAC
// 수집 실패, CPU serial 미지원 환경에서도 fingerprint 산출 가능해야.
func TestCompute_empty_fields_OK(t *testing.T) {
	in := Inputs{
		EKCert:    nil,
		MACs:      nil,
		CPUSerial: "",
		Salt:      []byte("tenant-salt"),
	}

	fp, err := Compute(in)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if fp.Hash == "" {
		t.Errorf("empty fields이라도 hash는 산출되어야 (salt만으로)")
	}
	if fp.HasTPM {
		t.Errorf("nil EKCert인데 HasTPM=true")
	}
	if fp.MACCount != 0 {
		t.Errorf("nil MACs인데 MACCount=%d", fp.MACCount)
	}
	if fp.Length != sha256.Size {
		t.Errorf("Length=%d, want %d", fp.Length, sha256.Size)
	}
}

// TestCompute_ErrSaltRequired_빈_salt_거부
//
// salt는 tenant 분리의 핵심 — 빈 salt는 cross-tenant 보호 불가하므로 거부.
func TestCompute_ErrSaltRequired_빈_salt_거부(t *testing.T) {
	cases := []struct {
		name string
		in   Inputs
	}{
		{"nil salt", Inputs{Salt: nil}},
		{"empty slice salt", Inputs{Salt: []byte{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compute(tc.in)
			if !errors.Is(err, ErrSaltRequired) {
				t.Errorf("want ErrSaltRequired, got %v", err)
			}
		})
	}
}

// TestCompute_CPUSerial_trim_lowercase
//
// design doc 결정론: CPUSerial trim + lowercase. " ABC123 " → "abc123"으로
// 정규화되어 같은 fingerprint 산출.
func TestCompute_CPUSerial_trim_lowercase(t *testing.T) {
	salt := []byte("salt")
	a := Inputs{CPUSerial: " ABC123 ", Salt: salt}
	b := Inputs{CPUSerial: "abc123", Salt: salt}
	c := Inputs{CPUSerial: "\tAbc123\n", Salt: salt}

	fpA, err := Compute(a)
	if err != nil {
		t.Fatalf("Compute a: %v", err)
	}
	fpB, err := Compute(b)
	if err != nil {
		t.Fatalf("Compute b: %v", err)
	}
	fpC, err := Compute(c)
	if err != nil {
		t.Fatalf("Compute c: %v", err)
	}

	if fpA.Hash != fpB.Hash {
		t.Errorf("trim 정규화 실패: %s vs %s", fpA.Hash, fpB.Hash)
	}
	if fpA.Hash != fpC.Hash {
		t.Errorf("whitespace trim 실패: %s vs %s", fpA.Hash, fpC.Hash)
	}
}

// TestCompute_hash_명세_알고리즘
//
// design doc §6.5 알고리즘 2:
//
//	fingerprint = sha256(EK_cert ‖ "|" ‖ sorted_macs ‖ "|" ‖ cpu_serial ‖ "|" ‖ salt)
//
// 본 테스트는 spec 그대로 외부 재현 가능성 확인.
func TestCompute_hash_명세_알고리즘(t *testing.T) {
	ek := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	macs := []string{"02:00:00:00:00:01", "01:00:00:00:00:02"}
	cpu := "Cpu-Serial-X"
	salt := []byte("tenant-salt")

	fp, err := Compute(Inputs{EKCert: ek, MACs: macs, CPUSerial: cpu, Salt: salt})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// 수동 재현: spec 그대로.
	sortedMACs := "01:00:00:00:00:02,02:00:00:00:00:01"
	normCPU := strings.ToLower(strings.TrimSpace(cpu))

	var buf bytes.Buffer
	buf.Write(ek)
	buf.WriteByte('|')
	buf.WriteString(sortedMACs)
	buf.WriteByte('|')
	buf.WriteString(normCPU)
	buf.WriteByte('|')
	buf.Write(salt)

	sum := sha256.Sum256(buf.Bytes())
	want := hex.EncodeToString(sum[:])

	if fp.Hash != want {
		t.Errorf("spec mismatch:\n got  %s\n want %s", fp.Hash, want)
	}
}

// TestCompute_empty_field_separator_보존
//
// design doc 결정론 노트: "모든 separator `|` byte literal — empty field도
// separator 보존". 빈 EKCert + 빈 MACs + 빈 CPUSerial = "|||<salt>"의
// sha256이어야 (separator 3개 보존).
func TestCompute_empty_field_separator_보존(t *testing.T) {
	salt := []byte("S")
	fp, err := Compute(Inputs{Salt: salt})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// 수동: "" | "" | "" | "S" → "|||S"
	expected := sha256.Sum256([]byte("|||S"))
	want := hex.EncodeToString(expected[:])

	if fp.Hash != want {
		t.Errorf("separator 보존 실패:\n got  %s\n want %s", fp.Hash, want)
	}
}

// TestCompute_MAC_중복_허용_정렬
//
// 같은 MAC이 여러 번 등장하면 (같은 인터페이스가 다중 link, alias) 그대로
// 정렬해 보존 — collector 단계가 dedupe 책임 (Compute는 결정론만 보장).
func TestCompute_MAC_중복_허용_정렬(t *testing.T) {
	salt := []byte("salt")
	a := Inputs{MACs: []string{"11:22:33:44:55:66", "11:22:33:44:55:66"}, Salt: salt}
	b := Inputs{MACs: []string{"11:22:33:44:55:66"}, Salt: salt}

	fpA, err := Compute(a)
	if err != nil {
		t.Fatalf("Compute a: %v", err)
	}
	fpB, err := Compute(b)
	if err != nil {
		t.Fatalf("Compute b: %v", err)
	}

	// MACCount는 입력 길이 그대로 반영 — dedupe 책임은 collector.
	if fpA.MACCount != 2 {
		t.Errorf("MACCount=%d, want 2", fpA.MACCount)
	}
	if fpB.MACCount != 1 {
		t.Errorf("MACCount=%d, want 1", fpB.MACCount)
	}
	// 결정론은 보장되나 dedupe 안 해서 hash는 다름 (의도).
	if fpA.Hash == fpB.Hash {
		t.Errorf("dedupe 책임은 collector — Compute는 입력 그대로 (hash 달라야)")
	}
}

// TestCompute_robot_위조_검출
//
// design doc §6.5 알고리즘 4: "다른 로봇이 결과 위조 시 fingerprint 불일치
// 즉시 검출". 두 로봇 inputs → 두 다른 fingerprint → fingerprint 검증으로
// 위조 검출 시나리오.
func TestCompute_robot_위조_검출(t *testing.T) {
	salt := []byte("tenant-X-salt")

	robot1 := Inputs{
		EKCert:    []byte{0x01, 0x02},
		MACs:      []string{"02:00:00:00:00:01"},
		CPUSerial: "robot-1-serial",
		Salt:      salt,
	}
	robot2 := Inputs{
		EKCert:    []byte{0x03, 0x04},
		MACs:      []string{"02:00:00:00:00:02"},
		CPUSerial: "robot-2-serial",
		Salt:      salt,
	}

	fp1, err := Compute(robot1)
	if err != nil {
		t.Fatalf("Compute robot1: %v", err)
	}
	fp2, err := Compute(robot2)
	if err != nil {
		t.Fatalf("Compute robot2: %v", err)
	}

	if fp1.Hash == fp2.Hash {
		t.Fatalf("두 로봇 fingerprint 같음 — 위조 검출 불가")
	}

	// 위조 시뮬레이션: robot2 audit 결과에 robot1 fingerprint를 첨부했다고 가정 →
	// 외부 검증자가 robot2 실 inputs로 재계산 → fp2와 일치 → robot1 fp 첨부물이 위조.
	fakeAttachedFP := fp1.Hash // 위조자가 첨부한 값
	verifyFP, err := Compute(robot2)
	if err != nil {
		t.Fatalf("Compute verify: %v", err)
	}
	if verifyFP.Hash == fakeAttachedFP {
		t.Errorf("위조 검출 실패 — 같은 hash가 나옴")
	}
}

// TestFingerprint_HasTPM_flag
//
// EKCert 비어있지 않으면 HasTPM=true, nil이면 false.
func TestFingerprint_HasTPM_flag(t *testing.T) {
	salt := []byte("s")

	withTPM, err := Compute(Inputs{EKCert: []byte{0x01}, Salt: salt})
	if err != nil {
		t.Fatalf("Compute withTPM: %v", err)
	}
	if !withTPM.HasTPM {
		t.Errorf("EKCert non-nil인데 HasTPM=false")
	}

	noTPM, err := Compute(Inputs{EKCert: nil, Salt: salt})
	if err != nil {
		t.Fatalf("Compute noTPM: %v", err)
	}
	if noTPM.HasTPM {
		t.Errorf("EKCert nil인데 HasTPM=true")
	}

	// 빈 slice (length 0)도 HasTPM=false (TPM 측에서 0-length cert는 발급 안 됨).
	emptyTPM, err := Compute(Inputs{EKCert: []byte{}, Salt: salt})
	if err != nil {
		t.Fatalf("Compute emptyTPM: %v", err)
	}
	if emptyTPM.HasTPM {
		t.Errorf("EKCert 빈 slice인데 HasTPM=true")
	}
}

// TestCompute_input_mutation_금지
//
// 원칙 §11 불변성: Compute는 입력 slice를 mutation하면 안 됨. 호출자가
// 입력 slice를 재사용해도 안전해야 (특히 MACs 정렬 시 입력 mutation 금지).
func TestCompute_input_mutation_금지(t *testing.T) {
	macs := []string{"zz:99:00:00:00:00", "aa:00:00:00:00:00"}
	original := make([]string, len(macs))
	copy(original, macs)

	_, err := Compute(Inputs{MACs: macs, Salt: []byte("s")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for i := range macs {
		if macs[i] != original[i] {
			t.Errorf("입력 MACs[%d] mutation: %q → %q", i, original[i], macs[i])
		}
	}
}

// =============================================================================
// v2 — D-3 robotid TPM Quote PCR 결합 보강 (R-D8 후속)
// =============================================================================
//
// v2 알고리즘 (design doc §6.5 5):
//
//   hash = sha256( EK_cert ‖ "|" ‖ sorted_macs ‖ "|" ‖ cpu_serial ‖ "|" ‖
//                  salt ‖ "|" ‖ pcr_digest )
//   pcr_digest = sha256( concat(PCR[i] for sorted i in PCRValues) )
//
// 결정론:
//   - PCRValues nil / empty → v1과 동일 (PCR 결합 없음, 회귀 0).
//   - PCRValues 1개 이상 → pcr_digest 계산 후 fingerprint에 결합.
//   - PCR index 정렬 후 ordered concat (map iteration 순서 invariant).
//
// Fingerprint 신규 필드:
//   - HasPCRQuote bool (PCR 결합 여부)
//   - PCRDigest string (hex hex, 디버깅·외부 재현용)

// TestCompute_v1_backward_compat_PCRValues_nil
//
// v1 호환 보장: Inputs.PCRValues nil → 기존 v1 fingerprint hash와 동일.
// v1 테스트가 v2 코드에서도 그대로 통과해야 (회귀 0).
func TestCompute_v1_backward_compat_PCRValues_nil(t *testing.T) {
	in := Inputs{
		EKCert:    []byte{0xDE, 0xAD, 0xBE, 0xEF},
		MACs:      []string{"02:00:00:00:00:01"},
		CPUSerial: "Cpu-Serial-X",
		Salt:      []byte("tenant-salt"),
		// PCRValues: nil — v1 호환 path.
	}

	fp, err := Compute(in)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// v1 spec 그대로 재현 (separator 3개).
	sortedMACs := "02:00:00:00:00:01"
	normCPU := strings.ToLower(strings.TrimSpace(in.CPUSerial))
	var buf bytes.Buffer
	buf.Write(in.EKCert)
	buf.WriteByte('|')
	buf.WriteString(sortedMACs)
	buf.WriteByte('|')
	buf.WriteString(normCPU)
	buf.WriteByte('|')
	buf.Write(in.Salt)

	sum := sha256.Sum256(buf.Bytes())
	want := hex.EncodeToString(sum[:])

	if fp.Hash != want {
		t.Errorf("v1 backward compat 위반:\n got  %s\n want %s", fp.Hash, want)
	}
	if fp.HasPCRQuote {
		t.Error("PCRValues nil인데 HasPCRQuote=true")
	}
	if fp.PCRDigest != "" {
		t.Errorf("PCRValues nil인데 PCRDigest=%q (want empty)", fp.PCRDigest)
	}
}

// TestCompute_v1_backward_compat_PCRValues_빈_map
//
// 빈 map (length 0)도 nil과 동일 path — HasPCRQuote=false + v1 hash와 동일.
func TestCompute_v1_backward_compat_PCRValues_빈_map(t *testing.T) {
	salt := []byte("salt")
	v1In := Inputs{EKCert: []byte{0x01}, Salt: salt}
	v1Empty := Inputs{EKCert: []byte{0x01}, Salt: salt, PCRValues: map[int][]byte{}}

	fpV1, err := Compute(v1In)
	if err != nil {
		t.Fatalf("Compute v1: %v", err)
	}
	fpEmpty, err := Compute(v1Empty)
	if err != nil {
		t.Fatalf("Compute empty: %v", err)
	}

	if fpV1.Hash != fpEmpty.Hash {
		t.Errorf("빈 map ≠ nil:\n got  %s\n want %s", fpEmpty.Hash, fpV1.Hash)
	}
	if fpEmpty.HasPCRQuote {
		t.Error("빈 PCRValues map인데 HasPCRQuote=true")
	}
}

// TestCompute_v2_PCR_결합_v1과_hash_다름
//
// PCRValues 채움 → fingerprint hash가 v1 (PCR 없음)과 달라야. 동일 EKCert·
// MACs·CPU·salt에서 PCR 추가가 hash에 영향 = "부팅 무결성까지 증명" 의도.
func TestCompute_v2_PCR_결합_v1과_hash_다름(t *testing.T) {
	salt := []byte("tenant-salt")
	base := Inputs{
		EKCert:    []byte{0xAA, 0xBB},
		MACs:      []string{"02:00:00:00:00:01"},
		CPUSerial: "robot-1",
		Salt:      salt,
	}

	v2 := base
	v2.PCRValues = map[int][]byte{
		0: {0x01, 0x02},
		2: {0x03, 0x04},
		4: {0x05, 0x06},
		7: {0x07, 0x08},
	}

	fpV1, err := Compute(base)
	if err != nil {
		t.Fatalf("Compute v1: %v", err)
	}
	fpV2, err := Compute(v2)
	if err != nil {
		t.Fatalf("Compute v2: %v", err)
	}

	if fpV1.Hash == fpV2.Hash {
		t.Errorf("PCR 추가가 hash에 영향 없음 — 부팅 무결성 결합 실패")
	}
	if !fpV2.HasPCRQuote {
		t.Error("PCRValues 채웠는데 HasPCRQuote=false")
	}
	if fpV2.PCRDigest == "" {
		t.Error("HasPCRQuote=true인데 PCRDigest 비어있음")
	}
	// PCRDigest는 sha256 hex (32바이트 → 64자).
	if len(fpV2.PCRDigest) != HashHexLength {
		t.Errorf("PCRDigest length=%d, want %d", len(fpV2.PCRDigest), HashHexLength)
	}
}

// TestCompute_v2_PCR_결정론
//
// 같은 PCR 값 → 같은 pcr_digest → 같은 fingerprint. 외부 검증자가
// 동일 PCR로 재현 가능해야.
func TestCompute_v2_PCR_결정론(t *testing.T) {
	salt := []byte("salt")
	pcr := map[int][]byte{
		0: {0x01, 0x02, 0x03},
		7: {0x07, 0x08, 0x09},
	}

	in1 := Inputs{Salt: salt, PCRValues: pcr}
	in2 := Inputs{Salt: salt, PCRValues: pcr}

	fp1, err := Compute(in1)
	if err != nil {
		t.Fatalf("Compute 1: %v", err)
	}
	fp2, err := Compute(in2)
	if err != nil {
		t.Fatalf("Compute 2: %v", err)
	}

	if fp1.Hash != fp2.Hash {
		t.Errorf("PCR 결정론 위반: %s vs %s", fp1.Hash, fp2.Hash)
	}
	if fp1.PCRDigest != fp2.PCRDigest {
		t.Errorf("PCRDigest 결정론 위반: %s vs %s", fp1.PCRDigest, fp2.PCRDigest)
	}
}

// TestCompute_v2_PCR_순서_invariant
//
// PCRValues는 map이라 iteration 순서 비결정 — 구현이 PCR index 정렬 후
// ordered concat해야. 다른 insertion 순서로 만든 두 map이 같은 fingerprint
// 산출해야.
func TestCompute_v2_PCR_순서_invariant(t *testing.T) {
	salt := []byte("salt")

	// Go map insertion order는 비결정이나, 같은 key+value 집합은 같은 결과여야.
	pcrA := map[int][]byte{
		7: {0x07},
		0: {0x00},
		4: {0x04},
		2: {0x02},
	}
	pcrB := map[int][]byte{
		0: {0x00},
		2: {0x02},
		4: {0x04},
		7: {0x07},
	}

	fpA, err := Compute(Inputs{Salt: salt, PCRValues: pcrA})
	if err != nil {
		t.Fatalf("Compute A: %v", err)
	}
	fpB, err := Compute(Inputs{Salt: salt, PCRValues: pcrB})
	if err != nil {
		t.Fatalf("Compute B: %v", err)
	}

	if fpA.Hash != fpB.Hash {
		t.Errorf("PCR 순서 invariant 위반 (정렬 후 concat 필요):\n A %s\n B %s",
			fpA.Hash, fpB.Hash)
	}
	if fpA.PCRDigest != fpB.PCRDigest {
		t.Errorf("PCRDigest 순서 invariant 위반: %s vs %s", fpA.PCRDigest, fpB.PCRDigest)
	}
}

// TestCompute_v2_PCR_값_다름_hash_다름
//
// PCR 값 1바이트 차이 → 다른 pcr_digest → 다른 fingerprint. 부팅 무결성
// 검출 핵심 시나리오 (rootkit이 boot loader 변조 시 PCR[4] 달라짐).
func TestCompute_v2_PCR_값_다름_hash_다름(t *testing.T) {
	salt := []byte("salt")

	cleanBoot := map[int][]byte{
		0: {0xAA, 0xBB, 0xCC},
		4: {0x11, 0x22, 0x33}, // clean boot loader PCR.
	}
	tamperedBoot := map[int][]byte{
		0: {0xAA, 0xBB, 0xCC},
		4: {0x11, 0x22, 0x34}, // 1바이트 변조.
	}

	fpClean, err := Compute(Inputs{Salt: salt, PCRValues: cleanBoot})
	if err != nil {
		t.Fatalf("Compute clean: %v", err)
	}
	fpTampered, err := Compute(Inputs{Salt: salt, PCRValues: tamperedBoot})
	if err != nil {
		t.Fatalf("Compute tampered: %v", err)
	}

	if fpClean.Hash == fpTampered.Hash {
		t.Errorf("PCR 1바이트 변조 검출 실패 — 같은 fingerprint")
	}
	if fpClean.PCRDigest == fpTampered.PCRDigest {
		t.Errorf("PCR 변조 → PCRDigest 같음 (검출 실패)")
	}
}

// TestCompute_v2_PCR_spec_알고리즘
//
// spec 그대로 외부 재현 가능성 — pcr_digest = sha256(concat(sorted PCR
// values))을 수동 재현하여 일치 확인.
func TestCompute_v2_PCR_spec_알고리즘(t *testing.T) {
	salt := []byte("tenant-salt")
	ek := []byte{0xDE, 0xAD}
	macs := []string{"02:00:00:00:00:01"}
	cpu := "robot-X"
	pcr := map[int][]byte{
		0: {0xA0, 0xA1},
		2: {0xB0, 0xB1},
		4: {0xC0, 0xC1},
		7: {0xD0, 0xD1},
	}

	fp, err := Compute(Inputs{
		EKCert: ek, MACs: macs, CPUSerial: cpu, Salt: salt, PCRValues: pcr,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// pcr_digest 수동 재현: sorted PCR index 순 (0, 2, 4, 7).
	pcrBuf := bytes.Buffer{}
	for _, i := range []int{0, 2, 4, 7} {
		pcrBuf.Write(pcr[i])
	}
	pcrSum := sha256.Sum256(pcrBuf.Bytes())
	wantPCRDigest := hex.EncodeToString(pcrSum[:])

	if fp.PCRDigest != wantPCRDigest {
		t.Errorf("PCRDigest spec mismatch:\n got  %s\n want %s",
			fp.PCRDigest, wantPCRDigest)
	}

	// 최종 fingerprint 수동 재현:
	//   sha256(EK ‖ "|" ‖ sorted_macs ‖ "|" ‖ normCPU ‖ "|" ‖ salt ‖ "|" ‖ pcr_digest_bytes)
	// pcr_digest는 raw 32바이트로 결합 (hex string 아님 — 결정론·byte 효율).
	var buf bytes.Buffer
	buf.Write(ek)
	buf.WriteByte('|')
	buf.WriteString("02:00:00:00:00:01")
	buf.WriteByte('|')
	buf.WriteString(strings.ToLower(strings.TrimSpace(cpu)))
	buf.WriteByte('|')
	buf.Write(salt)
	buf.WriteByte('|')
	buf.Write(pcrSum[:]) // raw bytes 결합.

	finalSum := sha256.Sum256(buf.Bytes())
	wantFP := hex.EncodeToString(finalSum[:])

	if fp.Hash != wantFP {
		t.Errorf("v2 spec mismatch:\n got  %s\n want %s", fp.Hash, wantFP)
	}
}

// TestCompute_v2_PCR_단일_값
//
// PCR 1개만 있어도 결합 동작 (정렬 후 concat이 단일 값 그대로).
func TestCompute_v2_PCR_단일_값(t *testing.T) {
	salt := []byte("salt")
	pcr := map[int][]byte{
		4: {0x11, 0x22, 0x33, 0x44},
	}

	fp, err := Compute(Inputs{Salt: salt, PCRValues: pcr})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if !fp.HasPCRQuote {
		t.Error("PCR 1개인데 HasPCRQuote=false")
	}

	// 수동: pcr_digest = sha256(0x11223344)
	wantSum := sha256.Sum256([]byte{0x11, 0x22, 0x33, 0x44})
	wantDigest := hex.EncodeToString(wantSum[:])
	if fp.PCRDigest != wantDigest {
		t.Errorf("단일 PCR digest mismatch:\n got  %s\n want %s",
			fp.PCRDigest, wantDigest)
	}
}

// TestCompute_v2_PCR_input_mutation_금지
//
// PCRValues map을 Compute가 mutation하면 안 됨 (특히 정렬 시 임시 slice).
// 호출자가 PCR map을 재사용해도 안전해야.
func TestCompute_v2_PCR_input_mutation_금지(t *testing.T) {
	salt := []byte("salt")
	original := map[int][]byte{
		0: {0x01, 0x02},
		7: {0x07, 0x08},
	}
	// snapshot 검증용 사본.
	snap := make(map[int][]byte, len(original))
	for k, v := range original {
		cp := make([]byte, len(v))
		copy(cp, v)
		snap[k] = cp
	}

	_, err := Compute(Inputs{Salt: salt, PCRValues: original})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// key 갯수 보존.
	if len(original) != len(snap) {
		t.Errorf("PCRValues key count mutated: %d → %d", len(snap), len(original))
	}
	// 각 key의 byte slice 보존.
	for k, v := range snap {
		got, ok := original[k]
		if !ok {
			t.Errorf("PCRValues key %d 삭제됨", k)
			continue
		}
		if !bytes.Equal(got, v) {
			t.Errorf("PCRValues[%d] mutation: %x → %x", k, v, got)
		}
	}
}

// TestFingerprint_v2_HasPCRQuote_flag
//
// HasPCRQuote는 PCRValues 비어있지 않을 때 true (HasTPM과 독립 — TPM 없어도
// PCR만 결합 가능 시나리오 대비 향후 확장 여지).
func TestFingerprint_v2_HasPCRQuote_flag(t *testing.T) {
	salt := []byte("salt")

	none, err := Compute(Inputs{Salt: salt})
	if err != nil {
		t.Fatalf("Compute none: %v", err)
	}
	if none.HasPCRQuote {
		t.Error("PCRValues nil인데 HasPCRQuote=true")
	}

	withPCR, err := Compute(Inputs{
		Salt:      salt,
		PCRValues: map[int][]byte{0: {0x00}},
	})
	if err != nil {
		t.Fatalf("Compute withPCR: %v", err)
	}
	if !withPCR.HasPCRQuote {
		t.Error("PCR 있는데 HasPCRQuote=false")
	}
}
