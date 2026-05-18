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
