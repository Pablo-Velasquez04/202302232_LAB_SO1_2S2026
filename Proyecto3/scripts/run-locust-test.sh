#!/usr/bin/env bash
cd "$(dirname "$0")/.."
export CARNET=202302232
locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 20 -r 10 -t 30s
