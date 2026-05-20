# Audit Rotation — S3 Cold Backend 운영 가이드

> **대상**: enterprise 빌드 (BSL 1.1) 운영자. AWS S3 또는 S3 호환 storage (MinIO·Wasabi·Backblaze B2)에 audit rotation cold archive를 저장하려는 경우.
> **선행**: rotation cron schedule 등록(`--audit-rotation-schedule`) + `make build-enterprise` 빌드.
> **참조**: `docs/design/notes/audit-chain-rotation-design.md` §D-AR-9 · `docs/onboarding/audit-rotation-verify.md`

---

## 1. 라이선스 + 빌드

S3 backend는 **BSL 1.1 enterprise** 라이선스 하에 배포됩니다 (`LICENSE-ENTERPRISE`). 코어 (Apache-2.0) 빌드는 file backend만 사용 가능합니다.

빌드 명령:

```bash
make build-enterprise
# → bin/rosshield-server-enterprise
```

또는 직접:

```bash
go build -tags rosshield_enterprise -o bin/rosshield-server-enterprise ./cmd/rosshield-server
```

코어 빌드에서 `--audit-cold-backend=s3`를 지정하면 부팅 시 다음 warning을 노출하고 file backend로 graceful fallback합니다:

```
audit rotation backend s3 requested but core build (no `rosshield_enterprise` tag) — falling back to file backend
```

---

## 2. AWS credential 로딩

S3Backend는 AWS SDK Go v2 **default credential chain**에 위임합니다. 명시 credential 입력은 받지 않습니다 (12-factor 일관 + secret 누출 risk 감소).

우선순위 (AWS SDK 표준):

1. 환경 변수 `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` + `AWS_SESSION_TOKEN`(옵션)
2. shared credentials file (`~/.aws/credentials`, profile은 `AWS_PROFILE` 또는 default)
3. IAM Roles for Service Accounts (IRSA, EKS)
4. EC2 instance profile / ECS task role

**권장 배포 패턴**: EC2/EKS 환경에서는 **3 또는 4 (IAM role)** — long-lived secret 없음 + 자동 rotation. 온프렘은 별 service account + 환경 변수.

---

## 3. flag·env 매핑

| flag                                | env                                          | 필수      | 예 |
| ----------------------------------- | -------------------------------------------- | --------- | --- |
| `--audit-cold-backend=s3`           | `ROSSHIELD_AUDIT_COLD_BACKEND=s3`            | (활성화)  | — |
| `--audit-s3-bucket=acme-audit`      | `ROSSHIELD_AUDIT_S3_BUCKET`                  | ✓         | `acme-audit-archives` |
| `--audit-s3-region=us-west-2`       | `ROSSHIELD_AUDIT_S3_REGION`                  | ✓         | `us-west-2` |
| `--audit-s3-prefix=tn_acme/`        | `ROSSHIELD_AUDIT_S3_PREFIX`                  | 옵션      | `audit-archives/tn_acme/` |
| `--audit-s3-endpoint=https://minio.local:9000` | `ROSSHIELD_AUDIT_S3_ENDPOINT`     | 옵션      | (MinIO/Wasabi 등) |
| `--audit-s3-force-path-style`       | `ROSSHIELD_AUDIT_S3_FORCE_PATH_STYLE=1`      | 옵션      | (MinIO 필요) |
| `--audit-s3-sse=AES256`             | `ROSSHIELD_AUDIT_S3_SSE`                     | 권장      | `AES256` 또는 `aws:kms` |
| `--audit-s3-kms-key-id=...`         | `ROSSHIELD_AUDIT_S3_KMS_KEY_ID`              | KMS 시    | `arn:aws:kms:us-west-2:123:key/...` |

flag 우선, flag 빈 값일 때만 env로 fallback. KMS key id처럼 secret성 값은 env 권장 (shell history 누출 회피).

---

## 4. IAM policy 예시

audit rotation 운영에 필요한 최소 권한 — `PutObject`, `GetObject`, `HeadObject`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "RosshieldAuditRotationReadWrite",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:HeadObject"
      ],
      "Resource": "arn:aws:s3:::acme-audit-archives/audit-archives/*"
    },
    {
      "Sid": "RosshieldAuditRotationListOptional",
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::acme-audit-archives",
      "Condition": {
        "StringLike": {"s3:prefix": ["audit-archives/*"]}
      }
    }
  ]
}
```

SSE-KMS 사용 시 추가:

```json
{
  "Sid": "RosshieldAuditRotationKMS",
  "Effect": "Allow",
  "Action": [
    "kms:Encrypt",
    "kms:GenerateDataKey",
    "kms:Decrypt"
  ],
  "Resource": "arn:aws:kms:us-west-2:123456789012:key/abc-def"
}
```

**원칙 (D-AR-9 + CLAUDE.md §보안)**: bucket 전체가 아닌 prefix(`audit-archives/*`)에만 권한 부여. 다른 운영 데이터와 격리.

---

## 5. SSE (Server-Side Encryption) 옵션

| 모드 | 키 관리 | 운영 부담 | 추가 비용 | 권장 케이스 |
| ---- | ------- | --------- | --------- | --------- |
| (none) | — | 0 | $0 | 개발·non-prod 만 |
| `AES256` (SSE-S3) | AWS 관리 | 0 | $0 | 일반 운영 default 권장 |
| `aws:kms` (SSE-KMS) | customer CMK | low (key rotation 자동) | KMS API 요청 + key 월 비용 | 컴플라이언스 (HIPAA·SOC2·PCI) |

**컴플라이언스 환경 권장**: `aws:kms` + customer-managed CMK + key rotation 활성 + CloudTrail KMS 로그 → 외부 감사인 증명 strong.

---

## 6. S3 호환 storage

AWS 외 S3 호환 storage 활용 시:

### MinIO (self-hosted)

```bash
--audit-cold-backend=s3 \
--audit-s3-bucket=audit-archives \
--audit-s3-region=us-east-1 \                # MinIO는 region 무관, default 채움
--audit-s3-endpoint=https://minio.internal:9000 \
--audit-s3-force-path-style \                # MinIO 필수
```

credential: env `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` (MinIO access key/secret).

### Wasabi (S3-compatible cloud)

```bash
--audit-s3-endpoint=https://s3.us-east-1.wasabisys.com \
--audit-s3-region=us-east-1 \
# ForcePathStyle 불요 (Wasabi virtual-hosted style 지원)
```

### Backblaze B2

```bash
--audit-s3-endpoint=https://s3.us-west-002.backblazeb2.com \
--audit-s3-region=us-west-002 \
```

---

## 7. URI 형식 + 검증

S3Backend가 `audit_rotation_segments.archive_uri`에 기록하는 URI 형식:

```
s3://<bucket>/<prefix><key>
```

예: `s3://acme-audit-archives/audit-archives/tn_acme/seg-000005.tar.gz`

`rosshield-audit-verify` CLI (E30)는 본 round에서 `s3://` URI를 직접 fetch하지 못합니다 (별 epic). 임시 운영: archive를 `aws s3 cp` 로 로컬에 다운로드 후 `file://` URI로 검증:

```bash
aws s3 cp s3://acme-audit-archives/audit-archives/tn_acme/seg-000005.tar.gz /tmp/
rosshield-audit-verify rotation \
  --archive-uri file:///tmp/seg-000005.tar.gz \
  --expected-segment-hash <hash from audit_rotation_segments>
```

---

## 8. 비용 추정 (참고)

가정: 100 tenant × tenant당 월 1회 rotation × archive 평균 100 MB → **월 10 GB 추가, 연 120 GB**.

| 항목 | AWS S3 Standard | S3 IA | Glacier Instant Retrieval |
| ---- | --------------- | ----- | ------------------------- |
| storage (월 120 GB) | $2.76 | $1.50 | $0.48 |
| PUT (월 100건) | $0.0005 | $0.001 | $0.002 |
| GET (감사인 분기 verify) | $0.0004/1000 | $0.001/1000 | $0.01/1000 |

연간 < $50 / 100 tenant 예상. 1 TB+ retention 시 S3 IA → Glacier 라이프사이클 정책 권장 — 단 verify latency tradeoff 검토 (Glacier Deep Archive는 retrieval 시간 12 hour+).

**본 round 비목표**: 라이프사이클 정책 자동 적용 — 별 epic (`audit-rotation-lifecycle-design.md`).

---

## 9. 운영 체크리스트

부팅 전:

- [ ] enterprise 빌드 (`make build-enterprise`)
- [ ] S3 bucket 생성 + IAM policy 적용 (§4)
- [ ] AWS credential 환경 준비 (IAM role 권장)
- [ ] SSE 모드 결정 (§5)

부팅 후 (첫 rotation tick 발생 시):

- [ ] log에 `audit rotation auto-schedule active backend=s3://...` 라인 노출 확인
- [ ] S3 bucket에 `seg-000001.tar.gz` 객체 출현 확인 (`aws s3 ls`)
- [ ] `audit_rotation_segments` 테이블의 `archive_uri` 값이 `s3://` scheme 확인
- [ ] 다운로드 + `rosshield-audit-verify rotation` PASS 확인

운영 중 주기:

- [ ] CloudTrail 또는 bucket access log 모니터 (예상 외 GET·DELETE 탐지)
- [ ] SSE-KMS key 사용량 모니터 (KMS API throttling 회피)

---

## 10. 트러블슈팅

| 증상 | 원인 후보 | 조치 |
| ---- | --------- | ---- |
| `s3 backend not available — build tag` 부팅 차단 | 코어 빌드 + `--audit-cold-backend=s3` | `make build-enterprise` 또는 file backend로 변경 |
| `AccessDenied` PUT 실패 | IAM policy prefix 불일치 | §4 policy 확인, prefix 정렬 |
| `RegionNotMatch` | bucket region ≠ `--audit-s3-region` | `aws s3api get-bucket-location` 확인 후 정렬 |
| MinIO에서 SignatureDoesNotMatch | `--audit-s3-force-path-style` 누락 | 옵션 추가 |
| KMS `AccessDeniedException` | KMS key resource policy + IAM 결합 부족 | §4 KMS policy 추가 |
| graceful fallback warning 후 file backend | 코어 빌드 감지 | 의도적이면 무시, 의도 외면 enterprise 빌드 |

---

## 11. 한계 (본 round 비목표)

- **lifecycle 정책 자동 적용** — S3 IA → Glacier 전환. 별 epic.
- **MinIO testcontainer 통합 테스트** — 본 round 단위 fake S3 only. CI/customer 환경 통합은 별 round.
- **multi-bucket / multi-region 동시 운영** — 단일 backend 1 bucket. multi-region HA는 E-MR.
- **verify CLI 직접 S3 fetch** — 본 round는 `aws s3 cp` 우회. CLI 통합은 E30 후속.
- **PUT idempotency 강화 (object lock + version)** — 본 round는 단순 PUT (덮어쓰기). compliance lock은 별 epic.
