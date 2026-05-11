// E31 — 코어 ↔ enterprise 경계 가드.
//
// 본 테스트는 코어 패키지(internal/{api,app,domain,platform})가 enterprise 패키지를
// 직접 import하지 않음을 강제합니다. enterprise 통합은 cmd/* bootstrap 또는 코어
// 인터페이스 + 어댑터 주입 경로로만 가능 (E32에서 채워짐).
//
// 본 파일은 build tag 무관 — 코어 빌드와 enterprise 빌드 양쪽에서 모두 실행됩니다.

package enterprise_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const enterpriseImportPrefix = "github.com/ssabro/rosshield/internal/enterprise/"

// 코어로 분류되는 패키지 prefix들. cmd/*는 bootstrap이므로 제외(enterprise import 가능).
var corePackagePrefixes = []string{
	"internal/api",
	"internal/app",
	"internal/domain",
	"internal/platform",
}

func TestCorePackagesDoNotImportEnterprise(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	violations := []string{}

	for _, corePrefix := range corePackagePrefixes {
		coreDir := filepath.Join(repoRoot, corePrefix)
		err := filepath.WalkDir(coreDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			for _, imp := range file.Imports {
				val := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(val, enterpriseImportPrefix) {
					rel, _ := filepath.Rel(repoRoot, path)
					violations = append(violations, rel+" → "+val)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", coreDir, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("코어 패키지가 enterprise package를 import 중 (도메인 경계 위반):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
