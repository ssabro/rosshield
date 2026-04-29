# E7 Blob Store — Filesystem Content-Addressed Storage Research

Phase 1 E7 evidence blob 저장소 설계 노트. 평문(zstd 미적용) + sha256 hex content addressing + 2-level shard. Windows·Linux·macOS 동시 지원이 전제이므로 `os.Rename` 의미·`fsync` 의미·case-insensitive FS 함정에 무게를 둔다.

전제:
- 경로 레이아웃: `<dataDir>/evidence/<hash[0:2]>/<hash[2:4]>/<hash>.blob`
- 인터페이스: `Store(ctx, reader) (sha string, size int64, err error)` / `Open(ctx, sha) (io.ReadCloser, error)` / `Verify(ctx, sha) error` / `Walk(ctx, fn func(sha string) error) error` / `Delete(ctx, sha) error`
- Phase 1 단순화: 압축 없음, 암호화 없음, 단일 노드(분산 X)
- 멀티테넌시는 `evidence_records` 메타 테이블에서 강제 — blob 자체는 hash로 dedup

---

## 1. Atomic Write Pattern (권장)

### 1.1 핵심 시퀀스

같은 디렉터리 안 temp 파일 → fsync → `os.Rename`. POSIX rename은 같은 파일시스템에서 atomic이 보장되며, Windows의 `MoveFileEx` 기반 `os.Rename`도 같은 볼륨 안에서 atomic이다(Go 1.5+ `MOVEFILE_REPLACE_EXISTING` 사용). **다른 볼륨/파일시스템 cross는 atomic 보장이 깨지므로 temp는 반드시 최종 dir과 같은 디렉터리에 만든다.**

```go
// 의사코드 (실제 구현 시 errcheck·defer 정리 추가)
func (s *FSStore) Store(ctx context.Context, src io.Reader) (string, int64, error) {
    // 1) staging dir 보장 (한 번만 MkdirAll, 결과 디렉터리는 hash 계산 후)
    stagingDir := filepath.Join(s.root, ".staging")
    if err := os.MkdirAll(stagingDir, 0o755); err != nil { return "", 0, err }

    // 2) staging temp 파일 생성 (O_EXCL 보장)
    tmp, err := os.CreateTemp(stagingDir, "blob-*.tmp")
    if err != nil { return "", 0, err }
    tmpPath := tmp.Name()
    cleanup := func() { _ = os.Remove(tmpPath) }
    defer func() { if err != nil { cleanup() } }()

    // 3) hash + size 누적하면서 스트리밍
    h := sha256.New()
    size, err := io.Copy(io.MultiWriter(tmp, h), src)
    if err != nil { tmp.Close(); return "", 0, err }

    // 4) fsync 파일 (Linux/macOS는 의미 있음, Windows는 FlushFileBuffers 호출됨)
    if err := tmp.Sync(); err != nil { tmp.Close(); return "", 0, err }
    if err := tmp.Close(); err != nil { return "", 0, err }

    // 5) 최종 경로 결정
    sha := hex.EncodeToString(h.Sum(nil))
    finalDir := filepath.Join(s.root, sha[0:2], sha[2:4])
    finalPath := filepath.Join(finalDir, sha+".blob")

    // 6) shard dir 보장 (race-safe — MkdirAll은 EEXIST 흡수)
    if err := os.MkdirAll(finalDir, 0o755); err != nil { return "", 0, err }

    // 7) dedup: 이미 존재하면 staging 파일 버리고 기존 사용
    if _, statErr := os.Stat(finalPath); statErr == nil {
        cleanup()
        return sha, size, nil
    }

    // 8) atomic rename
    if err := os.Rename(tmpPath, finalPath); err != nil {
        // Windows에서 reader가 finalPath를 동시에 열고 있으면 ERROR_SHARING_VIOLATION 가능
        // → 한 번 retry, 또는 finalPath 존재 재확인 후 무시
        if os.IsExist(err) || isWindowsSharingViolation(err) {
            if _, statErr := os.Stat(finalPath); statErr == nil {
                cleanup()
                return sha, size, nil
            }
        }
        return "", 0, err
    }

    // 9) (선택) 디렉터리 fsync — Linux 전용 의미. Windows·macOS는 noop 가까움
    if runtime.GOOS == "linux" {
        if d, derr := os.Open(finalDir); derr == nil {
            _ = d.Sync()
            _ = d.Close()
        }
    }
    return sha, size, nil
}
```

### 1.2 staging dir 분리 이유

같은 shard dir에 temp를 만들면 `Walk`가 `.tmp` 파일을 GC 후보로 잘못 인식할 위험. `.staging/`을 root 직속에 두고 `Walk`는 `<hash[0:2]>/<hash[2:4]>/` 패턴만 enumerate하면 격리된다. 단, `os.Rename` 시 `.staging` → `evidence/<aa>/<bb>/`는 같은 dataDir 볼륨이어야 atomic. dataDir이 mount 경계를 안 넘는다는 가정을 README에 명시.

### 1.3 Windows-specific

- `os.Rename`은 destination이 존재하면 Windows에서 실패하지만, Go 표준 라이브러리는 `MOVEFILE_REPLACE_EXISTING` 플래그로 덮어쓰기를 시도한다. **dedup 케이스에선 덮어쓰기를 원치 않으므로 rename 전에 stat → 존재 시 staging 삭제** 순서를 지킨다(코드 7단계).
- 동시성 race: 두 worker가 동시에 stat(없음) → rename → 후자는 전자의 결과를 덮어씀. 같은 bytes이므로 결과적으로 동일 파일이라 데이터 무결성은 OK이지만, 후자는 atomic 덮어쓰기 동안 reader가 짧게 EOF·SHARING_VIOLATION을 볼 수 있다. **Open 측에서 한 번 retry**가 가장 단순한 해결책.

---

## 2. Shard Layout 검증 (`<aa>/<bb>/<sha>.blob`)

### 2.1 256·256 = 65,536 dir 한도

- ext4·xfs·ntfs 모두 한 디렉터리에 수십만 엔트리를 견디지만, **enumerate(`readdir`) 비용은 O(n)**. shard 없이 flat 두면 `Walk` 시 수백만 stat 호출이 발생.
- 2-level shard로 평균 dir 당 파일 수 = 총 blob 수 / 65,536. 1M blob → dir 당 ~15개. 100M blob → dir 당 ~1,500개. Phase 1·2에서 충분.
- Phase 3+ (수십억 blob) 도달 시 3-level(`<aa>/<bb>/<cc>/`)로 확장. 마이그레이션 쉬움(`mv` 스크립트 + DB 경로 컬럼 갱신).

### 2.2 sha256 hex prefix 분포

sha256 출력은 사실상 균등 분포 → shard 한쪽으로 쏠림 없음. Git의 `objects/<aa>/<rest>` 1-level 패턴과 IPFS의 다단 shard 모두 hex prefix 균등 분포에 의존. 같은 패턴 채택.

### 2.3 dir 생성 race

`os.MkdirAll`은 내부적으로 `EEXIST`를 흡수하므로 동시 호출 안전. 단, 권한 문제(`EACCES`)·상위 dir 부재(`ENOENT` — 보통 `MkdirAll`이 처리)·디스크 full(`ENOSPC`)은 명시 에러로 surface해야 한다.

---

## 3. Hash Verify on Read (T4)

### 3.1 패턴

```go
func (s *FSStore) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
    p := s.path(sha)
    f, err := os.Open(p)
    if err != nil { return nil, err }
    return &verifyingReader{f: f, want: sha, h: sha256.New()}, nil
}

// verifyingReader: Read 시 hash 누적, EOF 도달 시 sum 비교
// 중간에 닫히면 검증 못 한다 — caller가 끝까지 읽었는지가 책임
```

**옵션 A**: Open 시 lazy verify (위 코드). 큰 blob에서 메모리·시간 절약. 단, 부분 읽기로 닫히면 검증 누락.
**옵션 B**: 별도 `Verify(sha)` API — 전체 hash 재계산 후 mismatch면 corruption error. 백그라운드 fsck job·GC 직전·외부 검증 요청에 사용.
**옵션 C**: Open 자체가 첫 byte 전에 전체 hash 검증 — 안전하지만 10MiB blob에서 latency·메모리 부담.

권장: **A + B 조합**. 일상 read는 A로 cheap하게 통과시키고 끝까지 읽힌 경우 mismatch면 명시적 `ErrCorrupted` 반환. Audit·fsck 경로에서는 B를 명시 호출. C는 채택하지 않음(평균 비용 과다).

### 3.2 mismatch 처리 정책

- `ErrCorrupted` 발생 시 자동 삭제 금지 — quarantine dir(`<dataDir>/evidence/.quarantine/<sha>.blob`)로 이동 + audit log + alert.
- `evidence_records`에서 해당 blob 참조는 invalid 마킹, scan은 fail-fast.
- 자동 재생성 불가능(원본 stdout/stderr 휘발) — Phase 2에서 robot에게 재스캔 요청 워크플로우 검토.

---

## 4. GC·삭제 cascade 전략

### 4.1 Refcount 모델

`evidence_records(blob_sha PRIMARY KEY 아님)` — 한 blob을 N개 evidence가 참조 가능(dedup). DB 레벨 정확한 refcount 대신 **mark-and-sweep**:

1. **Mark phase**: `SELECT DISTINCT blob_sha FROM evidence_records` → in-memory set
2. **Sweep phase**: `Walk` 으로 디스크 enumerate → set에 없으면 삭제 후보
3. **Grace period**: 후보가 N hours(예: 24h) 이상 모든 활성 scan보다 오래된 mtime이면 실제 삭제. mtime이 최근이면 in-flight Store 중일 가능성 → skip.

### 4.2 동시 Store와 GC race

GC가 Mark 직후·Sweep 전에 새 Store가 진행되면 새 blob이 set에 없어 삭제될 수 있다. 방어책:

- **mtime 임계**: GC 시작 시각 기준 mtime > T-grace 인 blob은 무조건 skip
- **GC 실행 lock**: 단일 인스턴스 advisory lock(file lock with `LOCK_EX` on `<dataDir>/evidence/.gc.lock`)
- DB transaction snapshot으로 Mark phase 일관성 확보(SQLite는 단일 reader transaction으로 충분)

### 4.3 Delete 의미

- `Delete(sha)`는 GC 전용 내부 API로 표시 — 외부에서 직접 호출 금지(원칙 9 불변성).
- 삭제 시 빈 shard dir cleanup은 비용 대비 이득 적음 → skip(빈 dir 65,536개도 무해).

---

## 5. 함정·테스트 케이스 후보

### 5.1 Cross-platform 함정

| # | 함정 | OS | 대응 |
|---|---|---|---|
| F1 | `os.Rename` cross-volume 실패 | All | dataDir 내부 staging만 사용, 외부 path 입력 금지 |
| F2 | Windows ERROR_SHARING_VIOLATION on rename when reader open | Windows | Open 측 retry, 또는 mmap 회피 |
| F3 | Windows ERROR_ACCESS_DENIED on Delete when handle open | Windows | Delete 전 모든 handle close 보장, GC는 grace period로 회피 |
| F4 | Windows·macOS-default case-insensitive FS — `AbCd` ≡ `abcd` | Windows, macOS | sha hex는 항상 lowercase 정규화 강제, mixed case 입력은 reject |
| F5 | symbolic link·hardlink 침투 | All | Walk 시 `os.Lstat`으로 정규 파일만 처리, symlink는 무시 + audit log |
| F6 | `fsync` 의미 차이 — Linux는 dir fsync 필요, Windows·macOS는 미정의 동작 | All | `runtime.GOOS == "linux"`에서만 dir fsync |
| F7 | Disk full(`ENOSPC`)에 partial temp 잔재 | All | staging은 항상 `defer cleanup()`, GC가 `.staging/`도 mtime+grace로 sweep |
| F8 | NTFS·APFS reserved name(`CON`, `PRN`, `AUX`, `.`, `..`) | Windows | sha hex는 16진수만 → reserved name 충돌 불가, 하지만 `.staging`·`.quarantine`·`.gc.lock`은 `.` 시작이라 안전 |
| F9 | Windows path length 260자 한도 | Windows | dataDir 절대경로 + shard 2단 + 64자 sha + `.blob` = 충분히 짧음. 단, dataDir 자체가 깊으면 long path prefix(`\\?\`) 적용 검토 |
| F10 | macOS APFS atomic rename은 보장되지만 fsync는 F_FULLFSYNC 필요 | macOS | Phase 1에서는 `Sync()` 호출만, Phase 2에서 F_FULLFSYNC 옵션 검토 |
| F11 | Windows antivirus·OneDrive scanner가 staging file 잠금 | Windows | dataDir에서 Defender exclusion 권장 문서화, retry 로직 |

### 5.2 테스트 케이스 후보

T1. Store 같은 bytes 2회 → 같은 sha, 한 파일만 존재
T2. Store 동시(goroutine N=10) 같은 bytes → 정확히 1개 파일, 모두 같은 sha 반환
T3. Store 동시 다른 bytes (각 worker 다른 payload) → N개 파일 정확히 존재
T4. Store 후 Open → 끝까지 읽고 hash 검증 통과
T5. 디스크에서 blob 1바이트 변조 → Open이 ErrCorrupted 반환
T6. Verify(sha) 단독 호출 → 정상은 nil, 변조는 ErrCorrupted, 부재는 ErrNotFound
T7. Store 중 ctx cancel → temp 파일 잔재 없음
T8. Store reader가 io.ErrUnexpectedEOF → temp 파일 잔재 없음, 에러 propagate
T9. Open 직후 다른 worker가 Store(같은 sha)로 rename 덮어씀 → reader는 일관된 데이터(같은 bytes라 OK)
T10. Walk이 `.staging`·`.quarantine`·`.gc.lock`을 skip
T11. GC mark-sweep: refcount 0 blob을 grace period 후 삭제, 활성 blob은 보존
T12. GC와 동시 Store race → 새 blob은 mtime 임계로 skip되어 보존
T13. **Windows-only**: case-mixed sha hex 입력 reject (`AbCd...` → ErrInvalidSha)
T14. **Windows-only**: Open된 상태에서 Delete 시도 → 에러 또는 grace deferral
T15. **Windows-only**: ERROR_SHARING_VIOLATION 발생 시 retry로 복구
T16. Disk full simulation(작은 tmpfs) → ENOSPC 에러, 잔재 없음
T17. 10MiB blob streaming Store → 메모리 사용량 < 1MiB(전체 buffering 안 함)
T18. Symlink가 shard dir에 심어진 경우 Walk이 따라가지 않음
T19. dataDir이 read-only mount → Store가 명시적 EACCES 에러
T20. shard dir 권한 0o000 강제 → 명시적 EACCES, panic 없음

---

## 6. 결정 권장값 (R9-X)

| ID | 결정 | 권장값 | 근거 |
|---|---|---|---|
| **R9-1** | Atomic write 전략 | staging temp(같은 dataDir) → fsync(file) → MkdirAll(shard) → Rename | POSIX·NTFS 모두 같은 볼륨 rename atomic 보장. dir fsync는 Linux만 |
| **R9-2** | Shard 깊이 | 2-level (`<aa>/<bb>/`) | 65,536 dir로 1M~10M blob 규모 dir 당 enumerate 비용 안정. Git·IPFS 패턴 검증됨 |
| **R9-3** | Hash verify 타이밍 | Lazy on read(EOF 시 검증) + 명시 `Verify(sha)` API | 일상 read latency 보존, fsck·audit는 명시 호출 |
| **R9-4** | GC 모델 | Mark-and-sweep + grace period(24h) + advisory file lock | refcount 컬럼 없이 단순. race는 mtime 임계 + lock으로 차단 |
| **R9-5** | Sha hex 정규화 | 입력·저장 모두 lowercase 강제, mixed case는 ErrInvalidSha | Windows·macOS-default case-insensitive FS 충돌 방지 |

부속 결정:
- 평문 저장 (Phase 1) → zstd·암호화는 Phase 2 R10에서 재검토
- staging 위치는 `<dataDir>/evidence/.staging/`로 root 직속(shard 안 포함)
- Quarantine 위치는 `<dataDir>/evidence/.quarantine/`
- 거대 blob 정책: streaming 강제(`io.Copy`), 전체 in-memory buffer 금지
- Symlink·hardlink는 Walk에서 무시 + audit log("unexpected file type")

---

## 7. 미결 항목 (Phase 2로 이연)

- zstd 압축 + content-type 헤더(평문 vs 압축 구분 magic byte)
- envelope 암호화(per-tenant DEK, KEK는 별도 KMS)
- 분산 store(S3·MinIO 백엔드 추상화)
- macOS F_FULLFSYNC 옵션
- Windows long path(`\\?\`) prefix 자동 적용
- 백그라운드 fsck 스케줄러(전체 Verify 순회)
- 압축 후 dedup 효율 측정(평문 sha 기준 vs 압축 후 sha 기준)
