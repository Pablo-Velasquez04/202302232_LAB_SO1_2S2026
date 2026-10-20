use axum::{extract::State, http::StatusCode, routing::{get, post}, Json, Router};
use serde::{Deserialize, Serialize};
use std::env;

#[derive(Deserialize, Serialize)]
struct Report {
    country: String,
    warplanes_in_air: i32,
    warships_in_water: i32,
    timestamp: String,
}

#[derive(Clone)]
struct AppState {
    client: reqwest::Client,
    target: String,
}

async fn health() -> &'static str {
    "ok"
}

// Recibe el JSON de Locust (vía Gateway) y lo reenvía al Deployment de Go
async fn ingest(State(st): State<AppState>, Json(r): Json<Report>) -> (StatusCode, String) {
    match st.client.post(&st.target).json(&r).send().await {
        Ok(resp) => {
            let code = StatusCode::from_u16(resp.status().as_u16())
                .unwrap_or(StatusCode::BAD_GATEWAY);
            (code, resp.text().await.unwrap_or_default())
        }
        Err(e) => (StatusCode::BAD_GATEWAY, format!("go-api no disponible: {e}")),
    }
}

#[tokio::main]
async fn main() {
    let target = env::var("TARGET_URL")
        .unwrap_or_else(|_| "http://go-api:8081/report".to_string());
    let state = AppState { client: reqwest::Client::new(), target };

    let app = Router::new()
        .route("/", post(ingest))
        .route("/health", get(health))
        .with_state(state);

    let listener = tokio::net::TcpListener::bind("0.0.0.0:8080").await.unwrap();
    axum::serve(listener, app).await.unwrap();
}
