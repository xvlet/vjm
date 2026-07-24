# VJM E2E Tests (JMX Test Suite)

이 디렉토리는 VJM 엔진의 Apache JMeter 스펙 준수 여부를 검증하기 위한 E2E(End-to-End) 테스트 JMX 파일들을 포함하고 있습니다. 모든 테스트는 함께 제공되는 **Mock Echo Server (`echosvr`)** 와 연동되어 동작합니다.

---

## 1. Mock Echo Server 구동

JMX 테스트 시나리오는 실제 HTTP/WebSocket 요청을 통해 응답을 검증합니다. 테스트용 Mock 서버는 별도의 Docker 이미지로 제공됩니다.

테스트를 실행하기 전에 아래 명령어 중 하나를 선택하여 로컬에 에코 서버를 구동해주세요.

### 옵션 A: Docker Run 명령어
```bash
docker run -d --name vjm-echosvr -p 58080:58080 -p 58081:58081 ghcr.io/xvlet/echosvr:latest
```

### 옵션 B: Docker Compose 사용 시
`docker-compose.yml` 파일로 실행하려면 아래 내용을 저장 후 `docker-compose up -d`로 구동합니다.
```yaml
version: '3'
services:
  echosvr:
    image: ghcr.io/xvlet/echosvr:latest
    ports:
      - "58080:58080"
      - "58081:58081"
```

> **참고**: `echosvr`는 기본적으로 HTTP(58080 포트) 및 WebSocket(58081 포트)으로 수신된 요청의 바디(Body)를 100% 그대로 에코(Echo)하여 반환합니다. 이를 통해 VJM의 어설션(Assertion)과 추출기(Extractor) 기능을 테스트할 수 있습니다.

---

## 2. JMX 네이밍 규칙 및 결과 해석

VJM 엔진의 기능 검증을 위해, 테스트 샘플러는 두 가지 패턴으로 구분되어 작성되었습니다. 

* **`Req1_..._Pass` (정상 케이스)**
  * 올바른 페이로드를 보내어 성공해야 하는 테스트입니다.
  * 이 샘플러에서 에러가 발생하면 파서나 평가(Evaluator) 로직의 오류를 의미합니다.

* **`Req2_..._Fail` (에러 유도 케이스)**
  * 닫히지 않은 XML, 잘못된 XPath 경로, 잘못된 해시값 등을 전송하여 어설션(Assertion) 실패를 유도하는 테스트입니다.
  * 엔진이 비정상 응답을 올바르게 캐치하는지 검증합니다.
  * 따라서 테스트 종료 후 출력되는 Error Set에 `Req2_..._Fail` 관련 에러가 포함되는 것은 정상적인 동작입니다.

---

## 3. 테스트 실행 방법

VJM CLI 빌드 후, 특정 JMX 파일을 지정하여 테스트를 수행합니다.

```bash
# 전체 워크스페이스 빌드
make all

# 특정 JMX 테스트 실행 예시
vjm -t tests/assertions/test_xml_assertion.jmx
```
