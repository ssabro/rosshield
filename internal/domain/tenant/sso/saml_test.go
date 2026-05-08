package sso_test

// saml_test.go — E20-C SAML 통합 테스트.
//
// 시나리오:
//
//	T1 ParseSAMLConfig — 정상·필수 필드 누락 거부.
//	T2 BuildSAMLAuthURL — IdP SSO endpoint URL 생성 + RelayState query.
//	T3 VerifySAMLAssertion — self-signed cert + 직접 서명한 assertion → NameID + email 추출.
//	T4 VerifySAMLAssertion — 빈 응답 / invalid base64 거부.
//	T5 VerifySAMLAssertion — 다른 cert로 서명된 assertion 거부 (signature mismatch).

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
)

// === T1: ParseSAMLConfig ===

func TestParseSAMLConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", `{"idpEntityId":"https://idp/","ssoUrl":"https://idp/sso","acsUrl":"https://app/acs","idpCertPem":"-----BEGIN CERTIFICATE-----\nFAKE\n-----END CERTIFICATE-----"}`, false},
		{"missing idpEntityId", `{"ssoUrl":"https://idp/sso","acsUrl":"https://app/acs","idpCertPem":"x"}`, true},
		{"missing ssoUrl", `{"idpEntityId":"https://idp/","acsUrl":"https://app/acs","idpCertPem":"x"}`, true},
		{"missing acsUrl", `{"idpEntityId":"https://idp/","ssoUrl":"https://idp/sso","idpCertPem":"x"}`, true},
		{"missing idpCertPem", `{"idpEntityId":"https://idp/","ssoUrl":"https://idp/sso","acsUrl":"https://app/acs"}`, true},
		{"empty", "", true},
		{"invalid json", "not json", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := sso.ParseSAMLConfig(json.RawMessage(tc.in))
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// === T2: BuildSAMLAuthURL ===

func TestBuildSAMLAuthURL(t *testing.T) {
	t.Parallel()
	_, certPEM := mustGenerateRSACert(t, time.Now())

	cfg := sso.SAMLConfig{
		IdPEntityID: "https://idp.test/entity",
		SSOURL:      "https://idp.test/sso",
		ACSURL:      "https://app.test/acs",
		IdPCertPEM:  certPEM,
	}
	c := sso.NewSAMLClient()
	url, err := c.BuildSAMLAuthURL(cfg, "state-123")
	if err != nil {
		t.Fatalf("BuildSAMLAuthURL: %v", err)
	}
	if !strings.HasPrefix(url, "https://idp.test/sso") {
		t.Errorf("url = %q, want prefix https://idp.test/sso", url)
	}
	if !strings.Contains(url, "RelayState=state-123") {
		t.Errorf("url = %q, want RelayState=state-123 query", url)
	}
}

// === T3: VerifySAMLAssertion happy path ===

func TestVerifySAMLAssertionHappy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	priv, certPEM := mustGenerateRSACert(t, now)

	cfg := sso.SAMLConfig{
		IdPEntityID: "https://idp.test/entity",
		SSOURL:      "https://idp.test/sso",
		ACSURL:      "https://app.test/acs",
		AudienceURI: "https://app.test/acs",
		IdPCertPEM:  certPEM,
	}

	signed := mustSignSAMLResponse(t, priv, certPEM, samlResponseFixture("user@example.com", "user@example.com", now, cfg))
	c := &sso.SAMLClient{Now: func() time.Time { return now }}
	assert, err := c.VerifySAMLAssertion(cfg, signed)
	if err != nil {
		t.Fatalf("VerifySAMLAssertion: %v", err)
	}
	if assert.NameID != "user@example.com" {
		t.Errorf("NameID = %q, want user@example.com", assert.NameID)
	}
	if assert.Email != "user@example.com" {
		t.Errorf("Email = %q, want user@example.com", assert.Email)
	}
}

// === T4: 거부 케이스 ===

func TestVerifySAMLAssertionRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, certPEM := mustGenerateRSACert(t, time.Now())
	cfg := sso.SAMLConfig{
		IdPEntityID: "https://idp.test/entity",
		SSOURL:      "https://idp.test/sso",
		ACSURL:      "https://app.test/acs",
		IdPCertPEM:  certPEM,
	}
	c := sso.NewSAMLClient()
	for _, in := range []string{"", "not-base64-???"} {
		_, err := c.VerifySAMLAssertion(cfg, in)
		if err == nil {
			t.Errorf("VerifySAMLAssertion(%q) = nil, want error", in)
		}
	}
}

// === T5: cert mismatch ===

func TestVerifySAMLAssertionRejectsBadSignature(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	signerPriv, _ := mustGenerateRSACert(t, now)
	_, otherCertPEM := mustGenerateRSACert(t, now)

	// signerPriv로 서명했지만 SP는 otherCertPEM(다른 cert)로 검증 → 거부.
	cfg := sso.SAMLConfig{
		IdPEntityID: "https://idp.test/entity",
		SSOURL:      "https://idp.test/sso",
		ACSURL:      "https://app.test/acs",
		AudienceURI: "https://app.test/acs",
		IdPCertPEM:  otherCertPEM,
	}
	signed := mustSignSAMLResponse(t, signerPriv, otherCertPEM, samlResponseFixture("user@example.com", "", now, cfg))
	c := &sso.SAMLClient{Now: func() time.Time { return now }}
	_, err := c.VerifySAMLAssertion(cfg, signed)
	if err == nil {
		t.Fatal("VerifySAMLAssertion: want error, got nil")
	}
	if !errors.Is(err, sso.ErrSAMLInvalid) {
		t.Errorf("err = %v, want wrapped ErrSAMLInvalid", err)
	}
}

// === fixture helpers ===

// mustGenerateRSACert는 1024-bit RSA + self-signed X.509 cert를 생성합니다.
//
// validAt 기준 -1h ~ +365d 유효. test용 빠른 생성.
func mustGenerateRSACert(t *testing.T, validAt time.Time) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             validAt.Add(-time.Hour),
		NotAfter:              validAt.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	return priv, string(pemBytes)
}

// samlResponseFixture는 minimal SAML Response XML(서명 전)을 생성합니다.
//
// IssueInstant·NotOnOrAfter는 now 기준 ±5분. NameID = subject(이메일), email attribute optional.
func samlResponseFixture(subject, email string, now time.Time, cfg sso.SAMLConfig) string {
	notBefore := now.Add(-5 * time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	notOnOrAfter := now.Add(5 * time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	issueInstant := now.UTC().Format("2006-01-02T15:04:05Z")
	emailAttr := ""
	if email != "" {
		emailAttr = fmt.Sprintf(`<saml:Attribute Name="email"><saml:AttributeValue>%s</saml:AttributeValue></saml:Attribute>`, email)
	}
	return fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="resp-1" Version="2.0" IssueInstant="%s" Destination="%s">
  <saml:Issuer>%s</saml:Issuer>
  <samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>
  <saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="assert-1" Version="2.0" IssueInstant="%s">
    <saml:Issuer>%s</saml:Issuer>
    <saml:Subject>
      <saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">%s</saml:NameID>
      <saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
        <saml:SubjectConfirmationData NotOnOrAfter="%s" Recipient="%s"/>
      </saml:SubjectConfirmation>
    </saml:Subject>
    <saml:Conditions NotBefore="%s" NotOnOrAfter="%s">
      <saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction>
    </saml:Conditions>
    <saml:AuthnStatement AuthnInstant="%s" SessionIndex="sess-1"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement>
    <saml:AttributeStatement>%s</saml:AttributeStatement>
  </saml:Assertion>
</samlp:Response>`,
		issueInstant, cfg.ACSURL,
		cfg.IdPEntityID,
		issueInstant,
		cfg.IdPEntityID,
		subject,
		notOnOrAfter, cfg.ACSURL,
		notBefore, notOnOrAfter,
		cfg.AudienceURI,
		issueInstant,
		emailAttr,
	)
}

// mustSignSAMLResponse는 raw SAML response XML의 Assertion 요소를 enveloped 서명합니다.
//
// IdP-side signing 시뮬레이션 — privateKey + cert로 SignEnveloped 호출.
// 결과는 base64 인코딩되어 SP가 받는 형태로 반환.
func mustSignSAMLResponse(t *testing.T, priv *rsa.PrivateKey, certPEM string, rawXML string) string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(rawXML); err != nil {
		t.Fatalf("etree parse: %v", err)
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("decode cert PEM failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	ks := &inMemoryKeyStore{priv: priv, cert: cert.Raw}
	signCtx := dsig.NewDefaultSigningContext(ks)
	signCtx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")

	// Assertion element를 sign.
	assertion := doc.FindElement("//Assertion")
	if assertion == nil {
		t.Fatal("Assertion element not found")
	}
	signed, err := signCtx.SignEnveloped(assertion)
	if err != nil {
		t.Fatalf("SignEnveloped: %v", err)
	}
	parent := assertion.Parent()
	parent.RemoveChild(assertion)
	parent.AddChild(signed)

	out, err := doc.WriteToString()
	if err != nil {
		t.Fatalf("WriteToString: %v", err)
	}
	return base64.StdEncoding.EncodeToString([]byte(out))
}

// inMemoryKeyStore는 dsig.X509KeyStore 구현입니다 (서명용).
type inMemoryKeyStore struct {
	priv *rsa.PrivateKey
	cert []byte
}

func (s *inMemoryKeyStore) GetKeyPair() (*rsa.PrivateKey, []byte, error) {
	return s.priv, s.cert, nil
}
