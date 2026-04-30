package compliance

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// frameworksFS는 git commit YAML(R14-9)을 단일 바이너리에 포함시킵니다.
//
// 디렉터리 레이아웃:
//
//	frameworks/<framework>.yaml
//
// 통제 데이터는 코드·제목·요약(<=200자) + ReferenceURL만 — 본문은 공식 사이트(R14-2).
//
//go:embed frameworks/*.yaml
var frameworksFS embed.FS

// frameworkYAML은 YAML 파일 1개의 전체 구조입니다.
type frameworkYAML struct {
	Framework string           `yaml:"framework"`
	Version   string           `yaml:"version"`
	SourceURL string           `yaml:"sourceUrl"`
	Controls  []controlDefYAML `yaml:"controls"`
}

// controlDefYAML은 한 통제 항목의 YAML 표현입니다.
type controlDefYAML struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Summary      string   `yaml:"summary,omitempty"`
	ReferenceURL string   `yaml:"referenceUrl,omitempty"`
	MappedChecks []string `yaml:"mappedChecks,omitempty"`
}

// frameworkCacheEntry는 메모리 캐시 1건입니다.
type frameworkCacheEntry struct {
	controls []ControlDefinition
	version  string
	err      error
}

var (
	frameworkCacheMu sync.Mutex
	frameworkCache   = map[Framework]frameworkCacheEntry{}
)

// LoadFramework는 embed YAML에서 framework 정의를 로드합니다.
//
// 반환: (controls, version, error).
// 캐시: 첫 호출 시 메모리 로드 + 결과(에러 포함) 캐시. 이후 호출은 즉시 반환. thread-safe.
//
// 검증:
//   - YAML.framework이 인자 name과 일치
//   - 각 control은 ID·Title 필수, ID 중복 금지
//   - Summary는 200자 초과 금지(R14-2 저작권 안전)
func LoadFramework(name Framework) ([]ControlDefinition, string, error) {
	if err := ValidateFramework(name); err != nil {
		return nil, "", err
	}

	frameworkCacheMu.Lock()
	defer frameworkCacheMu.Unlock()

	if entry, ok := frameworkCache[name]; ok {
		return entry.controls, entry.version, entry.err
	}

	controls, version, err := loadFrameworkUncached(name)
	frameworkCache[name] = frameworkCacheEntry{controls: controls, version: version, err: err}
	return controls, version, err
}

func loadFrameworkUncached(name Framework) ([]ControlDefinition, string, error) {
	path := "frameworks/" + string(name) + ".yaml"
	data, err := frameworksFS.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("compliance: read framework yaml %q: %w", path, err)
	}

	var doc frameworkYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("compliance: parse framework yaml %q: %w", path, err)
	}

	if Framework(doc.Framework) != name {
		return nil, "", fmt.Errorf("compliance: framework yaml %q declares %q but expected %q",
			path, doc.Framework, name)
	}
	if strings.TrimSpace(doc.Version) == "" {
		return nil, "", fmt.Errorf("compliance: framework yaml %q has empty version", path)
	}
	if len(doc.Controls) == 0 {
		return nil, "", fmt.Errorf("compliance: framework yaml %q has no controls", path)
	}

	seen := make(map[string]struct{}, len(doc.Controls))
	out := make([]ControlDefinition, 0, len(doc.Controls))
	for i, c := range doc.Controls {
		if strings.TrimSpace(c.ID) == "" {
			return nil, "", fmt.Errorf("compliance: framework yaml %q control[%d] has empty id", path, i)
		}
		if strings.TrimSpace(c.Title) == "" {
			return nil, "", fmt.Errorf("compliance: framework yaml %q control[%d] (%s) has empty title", path, i, c.ID)
		}
		if _, dup := seen[c.ID]; dup {
			return nil, "", fmt.Errorf("compliance: framework yaml %q duplicate control id %q", path, c.ID)
		}
		seen[c.ID] = struct{}{}

		// R14-2 저작권 안전: 요약 200자 한도. Title은 자체 작성이므로 한도 없음.
		if n := len([]rune(c.Summary)); n > 200 {
			return nil, "", fmt.Errorf("compliance: framework yaml %q control %q summary %d chars > 200 (R14-2 저작권 안전)",
				path, c.ID, n)
		}

		out = append(out, ControlDefinition{
			ID:             c.ID,
			Title:          c.Title,
			Summary:        c.Summary,
			ReferenceURL:   c.ReferenceURL,
			MappedCheckIDs: append([]string(nil), c.MappedChecks...),
		})
	}

	return out, doc.Version, nil
}

// ResetFrameworkCache는 캐시를 초기화합니다 (테스트 전용).
//
// 운영 코드에서는 호출 금지 — embed FS는 빌드 시점에 고정되므로 캐시 무효화 사유 없음.
func ResetFrameworkCache() {
	frameworkCacheMu.Lock()
	defer frameworkCacheMu.Unlock()
	frameworkCache = map[Framework]frameworkCacheEntry{}
}
