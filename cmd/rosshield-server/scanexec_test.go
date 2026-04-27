package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
)

// TestMapEvalStatus는 benchmark.EvalStatus 3-값 → scan.Outcome 매핑을 검증합니다.
func TestMapEvalStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   benchmark.EvalStatus
		want scan.Outcome
	}{
		{benchmark.StatusPass, scan.OutcomePass},
		{benchmark.StatusFail, scan.OutcomeFail},
		{benchmark.StatusIndeterminate, scan.OutcomeIndeterminate},
		{benchmark.EvalStatus("UNKNOWN"), scan.OutcomeError}, // unknown → error fallback
		{benchmark.EvalStatus(""), scan.OutcomeError},
	}
	for _, tc := range cases {
		if got := mapEvalStatus(tc.in); got != tc.want {
			t.Errorf("mapEvalStatus(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestMaterialToAuthMethodPassword(t *testing.T) {
	t.Parallel()
	mat := robot.CredentialMaterial{
		Type: robot.CredentialTypePassword, Username: "u", Password: "secret",
	}
	auth, err := materialToAuthMethod(mat)
	if err != nil {
		t.Fatalf("materialToAuthMethod: %v", err)
	}
	if auth == nil {
		t.Fatal("auth is nil")
	}
}

func TestMaterialToAuthMethodPasswordEmpty(t *testing.T) {
	t.Parallel()
	mat := robot.CredentialMaterial{Type: robot.CredentialTypePassword, Username: "u"}
	if _, err := materialToAuthMethod(mat); err == nil {
		t.Error("expected error for empty password, got nil")
	}
}

func TestMaterialToAuthMethodPrivateKey(t *testing.T) {
	t.Parallel()
	pemBytes := generateEd25519PEM(t)
	mat := robot.CredentialMaterial{
		Type:          robot.CredentialTypePrivateKey,
		Username:      "u",
		PrivateKeyPEM: string(pemBytes),
	}
	auth, err := materialToAuthMethod(mat)
	if err != nil {
		t.Fatalf("materialToAuthMethod: %v", err)
	}
	if auth == nil {
		t.Fatal("auth is nil")
	}
}

func TestMaterialToAuthMethodPrivateKeyEmpty(t *testing.T) {
	t.Parallel()
	mat := robot.CredentialMaterial{Type: robot.CredentialTypePrivateKey, Username: "u"}
	if _, err := materialToAuthMethod(mat); err == nil {
		t.Error("expected error for empty PEM, got nil")
	}
}

func TestMaterialToAuthMethodPrivateKeyInvalid(t *testing.T) {
	t.Parallel()
	mat := robot.CredentialMaterial{
		Type:          robot.CredentialTypePrivateKey,
		Username:      "u",
		PrivateKeyPEM: "not a valid PEM",
	}
	if _, err := materialToAuthMethod(mat); err == nil {
		t.Error("expected error for invalid PEM, got nil")
	}
}

func TestMaterialToAuthMethodUnknownType(t *testing.T) {
	t.Parallel()
	mat := robot.CredentialMaterial{Type: "bogus", Username: "u"}
	if _, err := materialToAuthMethod(mat); err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

// TestBenchmarkEvaluatorAdapter는 evaluator 어댑터의 핵심 호출을 검증합니다.
func TestBenchmarkEvaluatorAdapter(t *testing.T) {
	t.Parallel()
	a := &benchmarkEvaluatorAdapter{}

	// Pass case — equals "ok".
	res, err := a.Evaluate([]byte(`{"op":"equals","expected":"ok"}`),
		scan.ExecResult{Stdout: []byte("ok"), ExitCode: 0})
	if err != nil {
		t.Fatalf("Evaluate pass: %v", err)
	}
	if res.Outcome != scan.OutcomePass {
		t.Errorf("Outcome = %s, want pass", res.Outcome)
	}

	// Fail case — equals "ok" but stdout is "no".
	res, err = a.Evaluate([]byte(`{"op":"equals","expected":"ok"}`),
		scan.ExecResult{Stdout: []byte("no"), ExitCode: 0})
	if err != nil {
		t.Fatalf("Evaluate fail: %v", err)
	}
	if res.Outcome != scan.OutcomeFail {
		t.Errorf("Outcome = %s, want fail", res.Outcome)
	}
}

func TestBenchmarkEvaluatorAdapterEmptyRule(t *testing.T) {
	t.Parallel()
	a := &benchmarkEvaluatorAdapter{}
	if _, err := a.Evaluate(nil, scan.ExecResult{}); err == nil {
		t.Error("expected error for empty ruleJSON, got nil")
	}
}

func TestBenchmarkEvaluatorAdapterInvalidJSON(t *testing.T) {
	t.Parallel()
	a := &benchmarkEvaluatorAdapter{}
	if _, err := a.Evaluate([]byte(`{not json`), scan.ExecResult{}); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// generateEd25519PEM은 테스트용 OpenSSH PEM ed25519 private key를 생성합니다.
func generateEd25519PEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("ssh.MarshalPrivateKey: %v", err)
	}
	return pem.EncodeToMemory(block)
}
