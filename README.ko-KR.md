<p align="center">
  <img src="https://img.shields.io/badge/vjm-Vegeta--JMeter%20Engine-4A90D9?style=for-the-badge&logo=apache-jmeter&logoColor=white" alt="vjm banner">
</p>

<h1 align="center">⚡ vjm — Vegeta-JMeter Engine</h1>

<p align="center">
  <b>JMeter 테스트 플랜으로 Vegeta의 압도적인 성능을 그대로 활용하세요.</b><br>
  Write with JMeter. Attack with Vegeta. Report with JMeter.
</p>

<p align="center">
  <a href="https://github.com/xvlet/vjm"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version"></a>
  <a href="https://github.com/xvlet/vjm/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/Platform-Linux%20%7C%20AIX-lightgrey?style=for-the-badge" alt="Platform">
  <img src="https://img.shields.io/badge/Arch-amd64%20%7C%20ppc64-blueviolet?style=for-the-badge" alt="Architecture">
  <img src="https://img.shields.io/badge/CGO-Disabled-orange?style=for-the-badge" alt="CGO Disabled">
</p>

---

## 개요

**vjm**은 [Apache JMeter](https://jmeter.apache.org/)의 `.jmx` 테스트 플랜과 리포팅 기능을 그대로 활용하면서, 실제 HTTP 부하 발생은 Go 기반의 고성능 도구인 [Vegeta](https://github.com/tsenart/vegeta)를 통해 수행하는 **브릿지 엔진**입니다.

JMeter는 강력한 테스트 시나리오 작성 도구이지만 JVM 기반 특성상 대규모 동시 접속에서 성능 한계가 있습니다. vjm은 이 한계를 넘어, JMeter의 풍부한 생태계(GUI, 함수, 리포트)를 보존하면서 수천 TPS 이상의 부하를 안정적으로 발생시킵니다.

```
┌─────────────────────┐     파싱      ┌───────────────┐    부하 발생   ┌──────────────┐
│   JMeter .jmx 파일  │ ──────────▶  │  vjm (Engine) │ ────────────▶ │    Vegeta    │
│  (테스트 플랜 작성)  │             └───────────────┘               └──────┬───────┘
└─────────────────────┘                                                     │ .bin 결과
                                                                            ▼
┌─────────────────────┐    HTML 리포트  ┌───────────────┐   JTL 변환  ┌──────────────┐
│   JMeter Dashboard  │ ◀──────────── │     vjm       │ ◀────────── │   .jtl 파일  │
│    (결과 확인)       │               └───────────────┘             └──────────────┘
└─────────────────────┘
```

---

## 주요 기능

<table>
<tr><td><b>🗂️ JMX 완전 파싱</b></td><td>JMeter <code>.jmx</code> 파일의 HTTPSamplerProxy, HeaderManager, ThreadGroup, UserDefinedVariables, UserParameters, HTTP Request Defaults(ConfigTestElement) 파싱 지원</td></tr>
<tr><td><b>🔧 JMeter 함수 평가</b></td><td><code>${__time(...)}</code>, <code>${__RandomString(...)}</code>, <code>${__P(...)}</code>, <code>${__eval(...)}</code>, <code>${__FileToString(...)}</code> 등 내장 함수 지원</td></tr>
<tr><td><b>⚡ Vegeta 기반 부하 발생</b></td><td>초당 수천 TPS를 처리하는 Vegeta 엔진을 사용. <code>-r</code> (Rate), <code>-d</code> (Duration), <code>-w</code> (Workers) 파라미터로 정밀 제어</td></tr>
<tr><td><b>📊 JTL 자동 변환</b></td><td>Vegeta 결과(binary <code>.bin</code>)를 JMeter가 읽을 수 있는 CSV JTL 포맷으로 자동 변환</td></tr>
<tr><td><b>📋 JMeter HTML 리포트</b></td><td>변환된 JTL로 JMeter의 대시보드 HTML 리포트를 자동 생성</td></tr>
<tr><td><b>🔁 리포트 단독 생성 모드</b></td><td>기존 <code>.bin</code> 또는 <code>.jtl</code> 파일로 언제든 리포트만 별도로 재생성 가능</td></tr>
<tr><td><b>📦 단일 바이너리 배포</b></td><td>CGO 비활성화, 외부 라이브러리 의존성 없음. Linux(amd64)와 AIX(ppc64) 크로스 빌드 지원</td></tr>
<tr><td><b>🧩 .properties 파일 지원</b></td><td>JMeter 스타일의 <code>.properties</code> 파일을 여러 개 지정하여 환경별 파라미터를 쉽게 관리</td></tr>
</table>

---

## 사전 요구사항

vjm을 실행하는 머신에 아래 도구들이 설치되어 있어야 합니다.

| 도구 | 용도 | 설치 확인 |
|------|------|----------|
| [Vegeta](https://github.com/tsenart/vegeta) | HTTP 부하 발생 엔진 | `vegeta -version` |
| [Apache JMeter](https://jmeter.apache.org/) | HTML 리포트 생성 (선택) | `$JMETER_HOME/bin/jmeter -v` |

```bash
# Vegeta 설치 예시 (Linux)
go install github.com/tsenart/vegeta@latest

# 또는 GitHub Releases에서 바이너리 직접 다운로드
# https://github.com/tsenart/vegeta/releases
```

> **참고:** JMeter는 HTML 리포트(`-e` 옵션)를 생성할 때만 필요합니다. 부하 테스트 실행 자체에는 필요하지 않습니다.

---

## 빌드

```bash
git clone https://github.com/xvlet/vjm.git
cd vjm

# Linux (amd64) 빌드
make linux

# AIX (ppc64) 크로스 빌드
make aix

# 전체 빌드 (Linux + AIX)
make all

# 빌드 결과물 위치
ls build/
# vjm_linux   vjm_aix
```

### 수동 빌드

```bash
# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-w -s" -o vjm ./cmd/vjm/main.go

# AIX (PowerPC)
GOOS=aix GOARCH=ppc64 GOPPC64=power8 CGO_ENABLED=0 go build -ldflags="-w" -o vjm_aix ./cmd/vjm/main.go
```

---

## 빠른 시작

### 1. 부하 테스트 실행

```bash
# 기본 실행: JMX 파일 지정, 3000 TPS, 60초, 최대 200 워커
./vjm -t my_test.jmx -r 3000 -d 60s -w 200

# properties 파일을 여러 개 로드하여 환경 파라미터 주입
./vjm -t my_test.jmx \
      -p common.properties \
      -p headers.properties \
      -r 5000 -d 30s -w 300

# 결과 파일 경로를 직접 지정
./vjm -t my_test.jmx -r 1000 -d 10s -l ./results/my_result.bin
```

### 2. 부하 테스트 + HTML 리포트 동시 생성

```bash
./vjm -t my_test.jmx \
      -p common.properties \
      -r 3000 -d 60s -w 200 \
      -e ./html-report
```

실행 후 `./html-report/report_<timestamp>/index.html` 에서 JMeter 대시보드를 확인하세요.

### 3. 기존 결과 파일로 리포트만 생성

이미 `.bin` 또는 `.jtl` 파일이 있는 경우 부하 테스트 없이 리포트만 생성할 수 있습니다.

```bash
# .bin 파일로 JTL 변환 + HTML 리포트 생성
./vjm -g results/result_20260701_110632.bin -e ./html-report

# .jtl 파일이 이미 있는 경우: JTL 변환 생략, 리포트만 생성
./vjm -g results/result_20260701_110632.jtl -e ./html-report
```

---

## 옵션 레퍼런스

```
Usage: vjm -t <plan.jmx> [-p props.properties] -r 3000 -d 60s
       vjm -g <result.bin|result.jtl> -e <report_dir>

Options:
  -t string
        JMeter .jmx 파일 경로 (부하 테스트 모드 필수)

  -r, -rate int
        초당 요청 수 (TPS). 기본값: 1000

  -d, -duration string
        테스트 지속 시간. 예: 30s, 1m, 5m. 기본값: 30s

  -w, -workers int
        최대 동시 워커 수. 0이면 Vegeta 기본값 사용

  -p string
        .properties 파일 경로. 여러 번 지정 가능
        예: -p common.properties -p headers.properties

  -l string
        결과 바이너리(.bin) 저장 경로.
        기본값: results/result_YYYYMMDD_HHMMSS.bin

  -e, -export string
        HTML 리포트 출력 디렉토리.
        리포트는 <dir>/report_<timestamp>/ 하위에 생성됨

  -g, -report-only string
        기존 .bin 또는 .jtl 파일에서 리포트만 생성.
        -e 옵션과 함께 사용 필수

  -jmeter-home string
        JMETER_HOME 경로. 환경변수 $JMETER_HOME 자동 참조
```

---

## 출력 파일 구조

테스트 실행 후 다음 파일들이 생성됩니다.

```
results/
├── result_20260701_110632.bin    # Vegeta 바이너리 결과 (원본)
└── result_20260701_110632.jtl    # JMeter 호환 CSV (JTL 포맷)

html-report/
└── report_20260701_110632/
    ├── index.html                # JMeter 대시보드 메인 페이지
    ├── content/
    │   ├── pages/                # 세부 통계 페이지
    │   └── js/                   # 차트 데이터
    └── sbadmin2-1.0.7/           # 대시보드 CSS/JS
```

---

## .properties 파일 형식

JMeter 표준 properties 파일 형식을 그대로 사용합니다.

```properties
# common.properties
target.host=127.0.0.1
target.port=9998
target.path=/api/v1/testapi

# JMeter 함수 내에서 ${__P(target.host)} 형태로 참조
```

```properties
# headers.properties
http-header-name1=HEADER-DATA-1
someheader=somedata
testdata=test
```

---

## JMeter 함수 지원

`.jmx` 파일 내에서 사용하는 JMeter 표준 함수들을 평가합니다.

| 함수 | 설명 | 예시 |
|------|------|------|
| `${__time(format)}` | 현재 시각. 인자 없으면 Unix ms 반환 | `${__time(yyyyMMdd)}` |
| `${__RandomString(len,chars)}` | 랜덤 문자열 생성 | `${__RandomString(10,ABC123)}` |
| `${__P(key,default)}` | properties 값 참조 | `${__P(target.host,localhost)}` |
| `${__eval(expr)}` | 표현식 재평가 | `${__eval(${myVar})}` |
| `${__FileToString(path)}` | 파일 내용을 문자열로 로드 | `${__FileToString(body.json)}` |
| `${varName}` | 변수 참조 | `${target.host}` |

---

## 아키텍처

```
cmd/vjm/
└── main.go                  # CLI 엔트리포인트, 플래그 파싱

internal/
├── domain/
│   ├── entity.go            # TestConfig, RequestTemplate 도메인 모델
│   └── plan.go              # TestPlan, ThreadGroup, Sampler 도메인 모델
│
├── evaluator/
│   ├── evaluator.go         # Evaluator 인터페이스
│   └── jmeter_evaluator.go  # JMeter 함수/변수 평가기 구현
│
├── infra/
│   ├── parser/
│   │   └── jmx_parser.go    # JMX XML 파서 (SAX 스타일 스트리밍)
│   ├── vegeta/
│   │   └── runner.go        # Vegeta 프로세스 실행 및 스트리밍 타겟 공급
│   └── jmeter/
│       └── reporter.go      # Vegeta CSV → JTL 변환 / JMeter 리포트 호출
│
└── usecase/
    ├── interfaces.go        # StressTestUsecase, JmxParser 등 포트 인터페이스
    └── orchestrator.go      # 유스케이스 구현체 (Execute, GenerateReportOnly)
```

---

## AIX 환경 실행

AIX PowerPC 환경에서의 실행 팁입니다.

```bash
# asyncpreemptoff=1: 구버전 Go에서 AIX 시그널 처리 안정화
GODEBUG=asyncpreemptoff=1 ./vjm_aix \
    -t test.jmx \
    -p common.properties \
    -r 3000 -d 60s -w 200
```

### AIX 네트워크 튜닝 권장 설정

대규모 TPS에서 성능을 극대화하려면 root 권한으로 아래 설정을 적용하세요.

```bash
no -p -o rfc1323=1             # TCP Window Scaling 활성화
no -p -o tcp_recvspace=262144  # TCP 수신 버퍼 256KB
no -p -o tcp_sendspace=262144  # TCP 송신 버퍼 256KB
no -p -o sb_max=4194304        # 소켓 버퍼 최대 4MB
no -p -o somaxconn=8192        # 소켓 백로그 큐 확장
no -p -o tcp_ephemeral_low=10241  # 임시 포트 범위 확장
```

---

## 테스트 결과 예시

```
===================================================
Vegeta Attack Report:
===================================================
Requests      [total, rate, throughput]         75326, 7532.26, 7506.49
Duration      [total, attack, wait]             10.035s, 10s, 34.332ms
Latencies     [min, mean, 50, 90, 95, 99, max]  1.839ms, 51.648ms, 49.853ms, 77.117ms, 86.962ms, 110.445ms, 208.217ms
Bytes In      [total, mean]                     63424492, 842.00
Bytes Out     [total, mean]                     63424492, 842.00
Success       [ratio]                           100.00%
Status Codes  [code:count]                      200:75326
Error Set:
===================================================
```

---

## 로드맵

- [ ] **SteppingThreadGroup 지원**: JMeter의 계단식 부하 증가 시나리오 구현
- [ ] **다중 Sampler 지원**: ThreadGroup 내 여러 HTTPSampler를 가중치 기반으로 처리
- [ ] **JMeter CSV DataSet 지원**: `CSVDataSet`에서 요청별 다른 파라미터 주입
- [ ] **WebSocket 지원**: WS 프로토콜 부하 테스트 연동
- [ ] **실시간 콘솔 대시보드**: 테스트 진행 중 실시간 TPS / 응답시간 모니터링

---

## 라이선스

MIT License — see [LICENSE](LICENSE) for details.

---

<p align="center">
  <b>vjm</b> — Write with JMeter. Attack with Vegeta. ⚡
</p>
