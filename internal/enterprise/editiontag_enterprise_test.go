//go:build rosshield_enterprise

// E31 — enterprise 빌드 표면 sanity test.
//
// 본 테스트는 enterprise build tag를 켰을 때 7 placeholder 패키지의 EditionTag가
// 모두 "enterprise"로 빌드되는지 검증합니다. 코어 빌드(`go test ./...`)에서는
// 본 파일이 컴파일에서 제외되므로 영향 없음.

package enterprise_test

import (
	"testing"

	"github.com/ssabro/rosshield/internal/enterprise/crosswitness"
	"github.com/ssabro/rosshield/internal/enterprise/fleetxval"
	"github.com/ssabro/rosshield/internal/enterprise/multihash"
	"github.com/ssabro/rosshield/internal/enterprise/robotid"
	"github.com/ssabro/rosshield/internal/enterprise/rostopo"
	"github.com/ssabro/rosshield/internal/enterprise/selectdisclose"
	"github.com/ssabro/rosshield/internal/enterprise/wasmrt"
)

func TestEnterpriseEditionTagsAllPresent(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"crosswitness", crosswitness.EditionTag},
		{"selectdisclose", selectdisclose.EditionTag},
		{"multihash", multihash.EditionTag},
		{"wasmrt", wasmrt.EditionTag},
		{"robotid", robotid.EditionTag},
		{"rostopo", rostopo.EditionTag},
		{"fleetxval", fleetxval.EditionTag},
	}
	for _, c := range cases {
		if c.got != "enterprise" {
			t.Errorf("%s.EditionTag = %q, want %q", c.name, c.got, "enterprise")
		}
	}
}
