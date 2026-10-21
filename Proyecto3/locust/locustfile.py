import json
import os
import random
from datetime import datetime, timezone

from locust import HttpUser, task, between

CARNET = os.environ["CARNET"]

# Archivo descargado desde Zot como OCI Artifact (scripts/pull-config.sh)
with open(os.environ.get("LOCUST_CONFIG", "locust/config/countries.json")) as f:
    CFG = json.load(f)

COUNTRIES = CFG["countries"]
PLANES = CFG["ranges"]["warplanes_in_air"]
SHIPS = CFG["ranges"]["warships_in_water"]


class ReportUser(HttpUser):
    wait_time = between(0.01, 0.1)

    @task
    def send_report(self):
        payload = {
            "country": random.choice(COUNTRIES),
            "warplanes_in_air": random.randint(*PLANES),
            "warships_in_water": random.randint(*SHIPS),
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        self.client.post(f"/grpc-{CARNET}", json=payload)
