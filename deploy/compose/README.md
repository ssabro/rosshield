# rosshield Compose 배포 (E11)

## 개요

Docker Compose로 rosshield 온프렘 데모 환경을 단일 명령으로 띄웁니다. 단일 Go 바이너리(`rosshield-server`)와 SQLite 단일 노드 구성으로, Phase 1 데모·평가용입니다. (Postgres·HA 구성은 Phase 3 SKU에서 다룹니다.)

## 사전 요구사항

- Docker 24+ (BuildKit 기본 활성)
- Docker Compose v2 (`docker compose ...` 형태)
- 호스트 포트 8080 가용 (또는 `.env`에서 변경)

## 빠른 시작

```bash
cd deploy/compose
cp .env.example .env
# .env 편집 — admin 이메일·비밀번호·tenant 이름 입력
docker compose up -d --build
```

부팅 후 Web Console 접속: <http://localhost:8080>

상위 디렉터리에서는 Make 타깃으로도 가능합니다.

```bash
make compose-build
make compose-up
make compose-down
```

## 첫 부팅 흐름

1. `entrypoint.sh`가 `/var/lib/rosshield/data.db` 부재를 감지.
2. 환경변수 `ROSSHIELD_ADMIN_EMAIL`·`ROSSHIELD_ADMIN_PASSWORD`로 admin 시드 실행 (`rosshield-server seed admin ...`).
3. 시드 결과 JSON은 stdout에 출력 — `docker compose logs rosshield`로 확인.
4. 마지막에 `exec rosshield-server`로 server 기동(PID 1 신호 처리).

두 번째 부팅부터는 `data.db`가 존재하므로 시드를 건너뜁니다.

## 데이터 영속

- Named volume `rosshield-data` → 컨테이너 내부 `/var/lib/rosshield` 마운트.
- SQLite WAL 파일·서명 키·evidence blob이 모두 이 볼륨에 저장.
- `docker compose down`은 컨테이너만 종료 (볼륨 보존).
- `docker compose down -v`는 볼륨까지 삭제 (데모 리셋 시).

## 헬스체크

- Dockerfile `HEALTHCHECK`: 30초 간격으로 `wget --spider http://localhost:8080/healthz`.
- Compose가 컨테이너 상태를 `healthy`로 표시.
- `docker compose ps`로 상태 확인.

## 시연

브라우저에서 <http://localhost:8080> 접속 → admin 계정으로 로그인.

## 트러블슈팅

### admin 비밀번호를 잘못 입력했다

볼륨을 삭제하고 다시 부팅합니다 (데이터가 모두 사라집니다 — 데모 환경 한정).

```bash
docker compose down -v
# .env에서 비밀번호 수정 후
docker compose up -d
```

### 환경 변수가 누락됐다

Compose가 즉시 실패하며 메시지를 띄웁니다(`ROSSHIELD_ADMIN_EMAIL must be set in .env`). `.env`를 확인하세요.

### 헬스체크가 실패한다

```bash
docker compose logs rosshield --tail=50
docker compose exec rosshield wget -O- http://localhost:8080/healthz
```

## 백업·복원

### 백업 (volume tar 추출)

```bash
docker run --rm -v rosshield-data:/data -v "$PWD":/backup alpine \
    tar -czf /backup/rosshield-backup-$(date +%Y%m%d).tar.gz -C /data .
```

### 복원

```bash
docker compose down
docker volume rm rosshield-data
docker volume create rosshield-data
docker run --rm -v rosshield-data:/data -v "$PWD":/backup alpine \
    tar -xzf /backup/rosshield-backup-YYYYMMDD.tar.gz -C /data
docker compose up -d
```

## 보안 노트 (Phase 1 단순화)

- TLS는 본 Compose 범위 외 — 프런트단에 reverse proxy(Caddy·nginx·Traefik)를 두고 종단 처리하는 것을 권장.
- `.env`는 평문 비밀번호를 포함하므로 권한을 600으로 제한하세요.
  ```bash
  chmod 600 .env
  ```
- 컨테이너는 비-root 유저(`rosshield`)로 실행됩니다.
- 영속 볼륨은 호스트 docker 데몬 권한에 의해 보호됩니다.
- 운영 환경에서는 비밀번호 관리에 secrets manager 또는 Compose secrets 도입을 권장 (Phase 2+).
