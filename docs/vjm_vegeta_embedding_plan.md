# vjm: Vegeta Library Embedding Refactoring Plan

이 문서는 `vjm`의 아키텍처를 기존 "외부 CLI 프로세스 호출(exec)" 방식에서 "내부 라이브러리 임포트(Embedding)" 방식으로 리팩토링하기 위한 계획서입니다.

## 1. 개요 (Overview)
*   **AS-IS**: `vjm`이 `exec.Command`를 통해 외부 `vegeta` CLI 바이너리를 실행하고, 표준 입출력(파이프)을 통해 JSON 타겟을 전달 및 결과를 파싱.
*   **TO-BE**: `github.com/tsenart/vegeta/v12/lib` 패키지를 `vjm`의 Go 모듈로 직접 가져와, 메모리 상에서 구조체 객체를 직접 주고받으며 부하를 발생시킴.

## 2. 기대 효과 (Expected Benefits)
*   **의존성 제거**: 사용자가 사전에 `vegeta`를 설치할 필요가 없어지며, `vjm` 단일 실행 파일 하나로 모든 것이 해결됨.
*   **성능 극대화**: JSON 직렬화/역직렬화 및 IPC(프로세스 간 통신) 파이프 버퍼링으로 인한 Syscall 오버헤드 제거.
*   **기능 확장성 확보**: Vegeta의 `Result` 객체를 직접 다루므로 JTL 변환이나 대시보드 처리가 훨씬 정교해지며, 장기적으로 JMeter의 상태 유지(Stateful) 기능들을 직접 구현할 수 있는 기반이 됨.

---

## 3. 리팩토링 단계 (Action Plan)

### Phase 1: Go Module 의존성 추가
*   `go get github.com/tsenart/vegeta/v12/lib` 실행하여 모듈 종속성 확보.

### Phase 2: Targeter (타겟 생성기) 재작성
*   **대상 파일**: `internal/infra/vegeta/runner.go`
*   기존의 `json.NewEncoder(stdin)` 루프를 제거.
*   `vegeta.Targeter` 인터페이스 규격(`func(*vegeta.Target) error`)에 맞춘 커스텀 Targeter 클로저 함수 구현.
*   파싱된 `plan.ThreadGroup`의 가중치(Weight) 분배 로직을 해당 Targeter 함수 내부에 이식.

### Phase 3: Attacker 및 Pacer 연결 (부하 발생 로직 변경)
*   **대상 파일**: `internal/infra/vegeta/runner.go`
*   `exec.Command("vegeta", "attack", ...)` 제거.
*   CLI 인자 `-rate` 및 `-duration`을 기반으로 `vegeta.ConstantPacer` 및 `time.Duration` 설정.
*   `vegeta.NewAttacker(vegeta.Workers(...))` 인스턴스 초기화.
*   `attacker.Attack(targeter, pacer, duration, "vjm-attack")` 메서드를 호출하여 부하 발생 시작.

### Phase 4: 대시보드 및 결과 처리 (Result Handling)
*   기존의 `stdout` 파이프 스캐너 및 `parseFastJSON` 파서 완전 제거.
*   `attacker.Attack()`이 리턴하는 `<-chan *vegeta.Result` 채널을 수신하는 `for` 루프 구현.
*   수신된 `*vegeta.Result` 객체를 이용하여:
    1.  실시간 Dashboard 통계(TPS, Latency 등) 업데이트 및 출력.
    2.  `vegeta.NewEncoder(file)`를 사용하여 `.bin` 바이너리 파일에 직접 기록 (또는 JTL 포맷으로 즉시 기록).

### Phase 5: (Future) JMeter 상태 유지(Stateful) 시나리오 지원 아키텍처 준비
*   기본 연동이 완료된 후, Vegeta의 기본 `Attacker`를 대체하는 `Custom Jmeter Attacker`를 자체 구현하여, 타이머(`ConstantTimer`) 대기 및 변수 체이닝(Extractor)을 제어할 수 있는 구조로 확장.

---

## 4. 체크리스트 및 검증 방법
- [x] `vegeta` CLI 바이너리가 없는 환경(예: 순수 Docker 컨테이너)에서 `vjm` 실행 시 정상적으로 부하가 발생하는가?
- [x] 9000 TPS 이상의 고부하 테스트에서 기존 대비 CPU 사용량이나 병목이 개선되었는가?
- [x] 실시간 대시보드 및 최종 Attack Report 요약이 기존과 동일하거나 더 정확하게 출력되는가?
- [x] 생성된 `.bin` 파일이 기존 리포트 생성 로직과 완벽히 호환되는가?
