// Package sso의 SAML 통합 (E20-C Phase 3).
//
// 책임:
//
//	- SAML provider config(JSON) 파싱: idpEntityId·ssoUrl·acsUrl·idpCertPem (필요시 metadataXml inline).
//	- gosaml2.SAMLServiceProvider 인스턴스화 + IdP cert store 빌드.
//	- BuildAuthURL: SP-initiated AuthnRequest URL 생성 (HTTP-POST binding은 별 도우미).
//	- VerifyAssertion: encoded SAMLResponse(base64 XML) → AssertionInfo (NameID + email + audience 검증).
//
// 외부 dep: russellhaering/gosaml2 + goxmldsig + beevik/etree.
//
// 비목표 (본 stage 외):
//
//	- IdP-initiated SSO (RelayState만 받는 케이스).
//	- SLO (Single Logout).
//	- Encrypted assertion (대부분의 SAML IdP는 평문 assertion + signature만).

package sso

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	saml2 "github.com/russellhaering/gosaml2"
	dsig "github.com/russellhaering/goxmldsig"
)

// SAMLConfig는 Provider.Config의 SAML schema입니다.
//
// IdPEntityID: IdP entityID (Issuer 값과 일치해야 함 — assertion Issuer 검증).
// SSOURL: IdP SingleSignOnService URL (HTTP-Redirect/POST binding).
// ACSURL: SP의 AssertionConsumerService URL (callback path).
// IdPCertPEM: IdP 인증서 PEM (assertion XML 서명 검증용 public key).
// AudienceURI: SP의 audience(보통 entityID와 동일). 빈 값이면 ACSURL 사용.
type SAMLConfig struct {
	IdPEntityID string `json:"idpEntityId"`
	SSOURL      string `json:"ssoUrl"`
	ACSURL      string `json:"acsUrl"`
	IdPCertPEM  string `json:"idpCertPem"`
	AudienceURI string `json:"audienceUri,omitempty"`
}

// ParseSAMLConfig는 Provider.Config(json.RawMessage)를 SAMLConfig로 unmarshal·검증합니다.
//
// 필수 필드 누락 시 ErrInvalidSAMLConfig.
func ParseSAMLConfig(raw json.RawMessage) (SAMLConfig, error) {
	var cfg SAMLConfig
	if len(raw) == 0 {
		return SAMLConfig{}, ErrInvalidSAMLConfig
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return SAMLConfig{}, fmt.Errorf("%w: %v", ErrInvalidSAMLConfig, err)
	}
	if strings.TrimSpace(cfg.IdPEntityID) == "" ||
		strings.TrimSpace(cfg.SSOURL) == "" ||
		strings.TrimSpace(cfg.ACSURL) == "" ||
		strings.TrimSpace(cfg.IdPCertPEM) == "" {
		return SAMLConfig{}, fmt.Errorf("%w: idpEntityId/ssoUrl/acsUrl/idpCertPem are required", ErrInvalidSAMLConfig)
	}
	return cfg, nil
}

// SAMLClient는 IdP 호출 + 검증 책임을 갖는 stateless helper입니다.
//
// 실 호출은 ServiceProvider 메서드에 위임하므로 본 wrapper는 cert store + Now 주입만.
type SAMLClient struct {
	// Now는 시각 주입 — 결정론 테스트용.
	Now func() time.Time
}

// NewSAMLClient는 default 설정 client를 반환합니다.
func NewSAMLClient() *SAMLClient {
	return &SAMLClient{Now: time.Now}
}

// SAMLAssertion은 검증된 assertion에서 추출한 최소 정보입니다.
//
// 본 도메인이 의존하는 것: NameID(external_subject), Email(attribute), SessionIndex(SLO 후속용).
type SAMLAssertion struct {
	NameID       string
	Email        string
	SessionIndex string
	Attributes   map[string][]string
}

// BuildSAMLAuthURL은 SP-initiated AuthnRequest URL을 반환합니다 (HTTP-Redirect binding).
//
// relayState는 callback에서 state correlation에 사용됩니다 (sso.LoginAttempt.State).
func (c *SAMLClient) BuildSAMLAuthURL(cfg SAMLConfig, relayState string) (string, error) {
	sp, err := buildServiceProvider(cfg)
	if err != nil {
		return "", err
	}
	url, err := sp.BuildAuthURL(relayState)
	if err != nil {
		return "", fmt.Errorf("%w: build auth URL: %v", ErrInvalidSAMLConfig, err)
	}
	return url, nil
}

// VerifySAMLAssertion은 base64 인코딩된 SAMLResponse XML을 검증하고 NameID·email을 추출합니다.
//
// 검증 항목 (gosaml2 위임):
//
//   - XML signature (IdPCertPEM으로 verify)
//   - Issuer match (cfg.IdPEntityID)
//   - Audience restriction (cfg.AudienceURI 또는 ACSURL)
//   - NotBefore / NotOnOrAfter (with leeway)
//
// 실패: ErrSAMLInvalid + 원인 wrap.
func (c *SAMLClient) VerifySAMLAssertion(cfg SAMLConfig, encodedResponse string) (SAMLAssertion, error) {
	if strings.TrimSpace(encodedResponse) == "" {
		return SAMLAssertion{}, fmt.Errorf("%w: empty response", ErrSAMLInvalid)
	}
	sp, err := buildServiceProvider(cfg)
	if err != nil {
		return SAMLAssertion{}, err
	}
	if c != nil && c.Now != nil {
		sp.Clock = dsig.NewFakeClockAt(c.Now())
	}
	info, err := sp.RetrieveAssertionInfo(encodedResponse)
	if err != nil {
		return SAMLAssertion{}, fmt.Errorf("%w: %v", ErrSAMLInvalid, err)
	}
	if info == nil {
		return SAMLAssertion{}, fmt.Errorf("%w: nil assertion info", ErrSAMLInvalid)
	}
	if info.WarningInfo != nil {
		// gosaml2가 검증 통과 후에도 경고를 반환하는 경우가 있어 명시적 거부.
		w := info.WarningInfo
		if w.InvalidTime || w.NotInAudience {
			return SAMLAssertion{}, fmt.Errorf("%w: invalidTime=%t notInAudience=%t",
				ErrSAMLInvalid, w.InvalidTime, w.NotInAudience)
		}
	}
	if strings.TrimSpace(info.NameID) == "" {
		return SAMLAssertion{}, fmt.Errorf("%w: missing NameID", ErrSAMLInvalid)
	}

	out := SAMLAssertion{
		NameID:       info.NameID,
		SessionIndex: info.SessionIndex,
		Attributes:   make(map[string][]string),
	}
	if info.Values != nil {
		for name, attr := range info.Values {
			vals := make([]string, 0, len(attr.Values))
			for _, v := range attr.Values {
				vals = append(vals, v.Value)
			}
			out.Attributes[name] = vals
			// email attribute 추출 — 표준 friendly name 우선.
			lower := strings.ToLower(name)
			if out.Email == "" && (lower == "email" ||
				strings.HasSuffix(lower, ":email") ||
				strings.HasSuffix(lower, "/emailaddress") ||
				lower == "urn:oid:0.9.2342.19200300.100.1.3") && len(vals) > 0 {
				out.Email = vals[0]
			}
		}
	}
	return out, nil
}

// buildServiceProvider는 SAMLConfig에서 gosaml2.SAMLServiceProvider를 만듭니다.
func buildServiceProvider(cfg SAMLConfig) (*saml2.SAMLServiceProvider, error) {
	cert, err := parseCertPEM(cfg.IdPCertPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: parse IdP cert: %v", ErrInvalidSAMLConfig, err)
	}
	store := &samlCertStore{certs: []*x509.Certificate{cert}}

	audience := cfg.AudienceURI
	if audience == "" {
		audience = cfg.ACSURL
	}

	return &saml2.SAMLServiceProvider{
		IdentityProviderSSOURL:      cfg.SSOURL,
		IdentityProviderIssuer:      cfg.IdPEntityID,
		AssertionConsumerServiceURL: cfg.ACSURL,
		ServiceProviderIssuer:       audience,
		AudienceURI:                 audience,
		IDPCertificateStore:         store,
		// SP 자체는 AuthnRequest 서명 안 함 (Phase 3 단순화).
		SignAuthnRequests: false,
		// 평문 assertion 가정 — encrypted assertion은 후속.
	}, nil
}

// samlCertStore는 dsig.X509CertificateStore 구현 (단일 cert).
type samlCertStore struct {
	certs []*x509.Certificate
}

// Certificates는 dsig.X509CertificateStore 인터페이스 메서드입니다.
func (s *samlCertStore) Certificates() ([]*x509.Certificate, error) {
	return s.certs, nil
}

// parseCertPEM은 PEM 인코딩된 X.509 인증서 1건을 파싱합니다.
func parseCertPEM(pemStr string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

// SAML sentinel error 추가.
var (
	ErrInvalidSAMLConfig = errors.New("sso: invalid SAML config")
	ErrSAMLInvalid       = errors.New("sso: SAML response invalid")
)
