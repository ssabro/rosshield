//go:build rosshield_enterprise && linux

package robotid

import "crypto/x509"

// marshalEKPublicForTest는 test 전용 helper — 실 EK public key DER 산출 path를
// 재현하기 위해 x509.MarshalPKIXPublicKey를 wrap합니다.
func marshalEKPublicForTest(pub interface{}) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}
