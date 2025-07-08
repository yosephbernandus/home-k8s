use std::env;
use warp::Filter;
use serde::Serialize;

#[derive(Serialize)]
struct Response {
    message: String,
    hostname: String,
    timestamp: String,
}

#[tokio::main]
async fn main() {
    let hostname = gethostname::gethostname()
        .into_string()
        .unwrap_or_else(|_| "unknown".to_string());

    let hello = warp::path::end()
        .map(move || {
            let response = Response {
                message: "Hello World from Rust! 🦀".to_string(),
                hostname: hostname.clone(),
                timestamp: chrono::Utc::now().to_rfc3339(),
            };
            warp::reply::json(&response)
        });

    let health = warp::path("health")
        .map(|| warp::reply::with_status("OK", warp::http::StatusCode::OK));

    let routes = hello.or(health);

    let port = env::var("PORT")
        .unwrap_or_else(|_| "8080".to_string())
        .parse::<u16>()
        .unwrap_or(8080);

    println!("Starting Rust server on port {}", port);
    warp::serve(routes)
        .run(([0, 0, 0, 0], port))
        .await;
}
