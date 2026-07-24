# VJM E2E Tests (JMX Test Suite)

This directory contains E2E (End-to-End) test JMX files to verify VJM engine's compliance with Apache JMeter specifications. All tests are designed to run in conjunction with the provided **Mock Echo Server (`echosvr`)**.

---

## 1. Running the Mock Echo Server

The JMX test scenarios verify responses through actual HTTP/WebSocket requests. The Mock server for testing is provided as a separate Docker image.

Please start the echo server locally by choosing one of the commands below before running the tests.

### Option A: Docker Run Command
```bash
docker run -d --name vjm-echosvr -p 58080:58080 -p 58081:58081 ghcr.io/xvlet/echosvr:latest
```

### Option B: Using Docker Compose
If you prefer to use a `docker-compose.yml` file, save the content below and run `docker-compose up -d`.
```yaml
version: '3'
services:
  echosvr:
    image: ghcr.io/xvlet/echosvr:latest
    ports:
      - "58080:58080"
      - "58081:58081"
```

> **Note**: The `echosvr` basically echoes back the body of requests received via HTTP (port 58080) and WebSocket (port 58081) exactly as it is (100%). Through this, you can test VJM's various Assertions and Extractors.

---

## 2. JMX Naming Convention & Result Interpretation

To verify the functions of the VJM engine, test samplers are categorized into two patterns.

* **`Req1_..._Pass` (Normal Case)**
  * This is a positive test that sends a correct payload and should succeed.
  * If an error occurs in this sampler, it indicates a bug in the parser or Evaluator logic.

* **`Req2_..._Fail` (Error Inducing Case)**
  * This test intentionally sends unclosed XML, wrong XPath paths, invalid hash values, etc., to induce an assertion failure.
  * It verifies whether the engine properly catches abnormal responses.
  * Therefore, it is normal for errors related to `Req2_..._Fail` to appear in the Error Set printed after the test ends.

---

## 3. How to Run Tests

After building the VJM CLI, specify a particular JMX file to run the test.

```bash
# Build the entire workspace
make all

# Example of running a specific JMX test
vjm -t tests/assertions/test_xml_assertion.jmx
```
