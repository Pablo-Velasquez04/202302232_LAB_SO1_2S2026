#!/usr/bin/env bash
cd "$(dirname "$0")/.."
export CARNET=202302232
locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 10 -r 5 -t 30s
