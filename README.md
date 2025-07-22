# Complete Production-Ready Kubernetes Setup Guide

## Prerequisites Setup

### 1. Enable Required MicroK8s Add-ons
```bash
# Enable essential add-ons
microk8s enable dns
microk8s enable registry
microk8s enable storage

# Configure kubectl alias
alias kubectl="microk8s kubectl"
echo 'alias kubectl="microk8s kubectl"' >> ~/.bashrc
source ~/.bashrc

# Enable MetalLB with proper IP range
# IMPORTANT: Choose IPs that don't conflict with your DHCP range
# For network 192.168.1.0/24, use range like 192.168.1.200-210
microk8s enable metallb:192.168.1.200-192.168.1.210

# Enable Traefik ingress controller (better for development than NGINX)
microk8s enable traefik

# Verify cluster is running
kubectl get nodes
kubectl get pods -A

# Verify MetalLB is working
kubectl get svc -n traefik
# Should show EXTERNAL-IP assigned from your range
```

### 2. Install Docker (if not already installed)
```bash
sudo apt update
sudo apt install docker.io -y
sudo usermod -aG docker $USER
newgrp docker
```

### 3. Configure Network Access
```bash
# Allow access from your local network
sudo ufw allow from 192.168.1.0/24

# Check your MetalLB IP assignment
kubectl get svc -n traefik
# Note the EXTERNAL-IP (e.g., 192.168.1.200)

# Update /etc/hosts for local access
sudo sed -i '/\.local/d' /etc/hosts
echo "192.168.1.200 go-hello.local rust-hello.local llm.local" | sudo tee -a /etc/hosts
```

## Goal 1: Go Hello World Web Application

### Step 1.1: Create Go Application
```bash
mkdir -p ~/k8s-apps/go-hello-world
cd ~/k8s-apps/go-hello-world
```

Create `main.go`:
```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
)

func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        hostname, _ := os.Hostname()
        fmt.Fprintf(w, "Hello World from Go! 🚀\nHostname: %s\nPath: %s\n", hostname, r.URL.Path)
    })

    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, "OK")
    })

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    log.Printf("Server starting on port %s", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

### Step 1.2: Create Dockerfile
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go mod init hello-world && go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
```

### Step 1.3: Build and Push to Local Registry
```bash
# Build and push the image
docker build -t localhost:32000/go-hello-world:latest .
docker push localhost:32000/go-hello-world:latest
```

### Step 1.4: Create Kubernetes Manifests
Create `go-hello-world-k8s.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: go-hello-world
  labels:
    app: go-hello-world
spec:
  replicas: 2
  selector:
    matchLabels:
      app: go-hello-world
  template:
    metadata:
      labels:
        app: go-hello-world
    spec:
      containers:
      - name: go-hello-world
        image: localhost:32000/go-hello-world:latest
        ports:
        - containerPort: 8080
        env:
        - name: PORT
          value: "8080"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "64Mi"
            cpu: "50m"
          limits:
            memory: "128Mi"
            cpu: "100m"
---
apiVersion: v1
kind: Service
metadata:
  name: go-hello-world-service
spec:
  selector:
    app: go-hello-world
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: go-hello-world-ingress
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  ingressClassName: traefik
  rules:
  - host: go-hello.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: go-hello-world-service
            port:
              number: 80
```

### Step 1.5: Deploy Go Application
```bash
kubectl apply -f go-hello-world-k8s.yaml

# Verify deployment
kubectl get pods -l app=go-hello-world
kubectl get svc go-hello-world-service
kubectl get ingress go-hello-world-ingress

# Test access
curl http://go-hello.local
```

## Goal 2: Rust Web Application

### Step 2.1: Create Rust Application
```bash
mkdir -p ~/k8s-apps/rust-hello-world
cd ~/k8s-apps/rust-hello-world
```

Create `Cargo.toml`:
```toml
[package]
name = "rust-hello-world"
version = "0.1.0"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
warp = "0.3"
serde = { version = "1.0", features = ["derive"] }
gethostname = "0.4"
chrono = { version = "0.4", features = ["serde"] }
```

Create `src/main.rs`:
```rust
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
```

### Step 2.2: Create Dockerfile (Updated Rust Version)
```dockerfile
FROM rust:1.83 as builder
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/target/release/rust-hello-world .
EXPOSE 8080
CMD ["./rust-hello-world"]
```

### Step 2.3: Build and Deploy Rust App
```bash
docker build -t localhost:32000/rust-hello-world:latest .
docker push localhost:32000/rust-hello-world:latest

# Create similar k8s manifest (same pattern as Go)
cp ../go-hello-world/go-hello-world-k8s.yaml rust-hello-world-k8s.yaml

# Update the manifest for Rust
sed -i 's/go-hello-world/rust-hello-world/g' rust-hello-world-k8s.yaml
sed -i 's/go-hello.local/rust-hello.local/g' rust-hello-world-k8s.yaml

kubectl apply -f rust-hello-world-k8s.yaml

# Test access
curl http://rust-hello.local
```

## Goal 3: Open WebUI for LLM Interface

### Step 3.1: Install and Configure Ollama
```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# IMPORTANT: Configure Ollama to listen on all interfaces (not just localhost)
sudo systemctl stop ollama
sudo mkdir -p /etc/systemd/system/ollama.service.d

cat | sudo tee /etc/systemd/system/ollama.service.d/override.conf << 'EOF'
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
EOF

# Reload and start Ollama
sudo systemctl daemon-reload
sudo systemctl start ollama
sudo systemctl enable ollama

# Verify Ollama is accessible from network
curl http://localhost:11434/api/version
SERVER_IP=$(hostname -I | awk '{print $1}')
curl http://$SERVER_IP:11434/api/version

# Download models
ollama pull codellama:7b
ollama pull qwen2.5-coder:7b
ollama list
```

### Step 3.2: Create Open WebUI Secret (YAML Approach)
```bash
mkdir -p ~/k8s-apps/open-webui
cd ~/k8s-apps/open-webui

# Create secret file (consistent with cloudflared approach)
cat > open-webui-secret.yaml << EOF
apiVersion: v1
data:
    webui-secret-key: '$(openssl rand -hex 32 | base64 -w 0)'
kind: Secret
metadata:
    name: open-webui-secret
    namespace: default
type: Opaque
EOF
```

### Step 3.3: Create Open WebUI Deployment
Create `open-webui-k8s.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: open-webui
  labels:
    app: open-webui
spec:
  replicas: 1
  selector:
    matchLabels:
      app: open-webui
  template:
    metadata:
      labels:
        app: open-webui
    spec:
      containers:
      - name: open-webui
        image: ghcr.io/open-webui/open-webui:main
        ports:
        - containerPort: 8080
        env:
        - name: OLLAMA_BASE_URL
          value: "http://192.168.1.253:11434"  # Replace with your server IP
        - name: WEBUI_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: open-webui-secret
              key: webui-secret-key
        - name: ENABLE_RAG_WEB_SEARCH
          value: "true"
        volumeMounts:
        - name: open-webui-data
          mountPath: /app/backend/data
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
        resources:
          requests:
            memory: "512Mi"
            cpu: "200m"
          limits:
            memory: "2Gi"  # Increased to prevent OOMKilled
            cpu: "1000m"
      volumes:
      - name: open-webui-data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: open-webui-service
spec:
  selector:
    app: open-webui
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: open-webui-ingress
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  ingressClassName: traefik
  rules:
  - host: llm.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: open-webui-service
            port:
              number: 80
```

### Step 3.4: Deploy Open WebUI
```bash
# Update server IP in deployment
SERVER_IP=$(hostname -I | awk '{print $1}')
sed -i "s/192.168.1.253/$SERVER_IP/g" open-webui-k8s.yaml

# Deploy secret and application
kubectl apply -f open-webui-secret.yaml
kubectl apply -f open-webui-k8s.yaml

# Monitor deployment
kubectl get pods -l app=open-webui -w

# Check logs for any issues
kubectl logs -l app=open-webui -f

# Test access
curl http://llm.local
```

## Goal 4: CUDA Tools Setup

### Step 4.1: Install NVIDIA Drivers
```bash
# Check NVIDIA GPU
lspci | grep -i nvidia

# Install NVIDIA drivers
sudo apt update
sudo apt install nvidia-driver-535 -y
sudo reboot

# Verify after reboot
nvidia-smi
```

### Step 4.2: Install NVIDIA Container Toolkit (Official Method)
```bash
# Configure repository
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

# Install toolkit
sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit

# Configure Docker runtime
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker

# Test CUDA access
docker run --rm --gpus all nvidia/cuda:12.9.1-cudnn-devel-ubuntu24.04 nvidia-smi
```

### Step 4.3: Enable GPU Support in MicroK8s
```bash
microk8s enable gpu

# Verify GPU nodes
kubectl describe nodes | grep nvidia
```

### Step 4.4: Create CUDA Test Application
```bash
mkdir -p ~/k8s-apps/cuda-test
cd ~/k8s-apps/cuda-test
```

Create `cuda-test.cu`:
```cuda
#include <stdio.h>
#include <cuda_runtime.h>

__global__ void vectorAdd(float *a, float *b, float *c, int n) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i < n) {
        c[i] = a[i] + b[i];
    }
}

int main() {
    printf("CUDA Vector Addition Test\n");
    printf("CUDA Version: %d.%d\n", CUDA_VERSION / 1000, (CUDA_VERSION % 100) / 10);
    
    int n = 1000000;
    size_t size = n * sizeof(float);
    
    float *h_a = (float*)malloc(size);
    float *h_b = (float*)malloc(size);
    float *h_c = (float*)malloc(size);
    
    for (int i = 0; i < n; i++) {
        h_a[i] = rand() / (float)RAND_MAX;
        h_b[i] = rand() / (float)RAND_MAX;
    }
    
    float *d_a, *d_b, *d_c;
    cudaMalloc(&d_a, size);
    cudaMalloc(&d_b, size);
    cudaMalloc(&d_c, size);
    
    cudaMemcpy(d_a, h_a, size, cudaMemcpyHostToDevice);
    cudaMemcpy(d_b, h_b, size, cudaMemcpyHostToDevice);
    
    int blockSize = 256;
    int numBlocks = (n + blockSize - 1) / blockSize;
    vectorAdd<<<numBlocks, blockSize>>>(d_a, d_b, d_c, n);
    
    cudaMemcpy(h_c, d_c, size, cudaMemcpyDeviceToHost);
    
    printf("CUDA Vector Addition Completed Successfully!\n");
    printf("First 10 results:\n");
    for (int i = 0; i < 10; i++) {
        printf("%.2f + %.2f = %.2f\n", h_a[i], h_b[i], h_c[i]);
    }
    
    cudaFree(d_a);
    cudaFree(d_b);
    cudaFree(d_c);
    free(h_a);
    free(h_b);
    free(h_c);
    
    return 0;
}
```

Create `Dockerfile`:
```dockerfile
FROM nvidia/cuda:12.9.1-cudnn-devel-ubuntu24.04

RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY cuda-test.cu .
RUN nvcc -o cuda-test cuda-test.cu

CMD ["./cuda-test"]
```

### Step 4.5: Deploy CUDA Test
```bash
docker build -t localhost:32000/cuda-test:latest .
docker push localhost:32000/cuda-test:latest

cat > cuda-test-k8s.yaml << 'EOF'
apiVersion: batch/v1
kind: Job
metadata:
  name: cuda-test-job
spec:
  template:
    spec:
      containers:
      - name: cuda-test
        image: localhost:32000/cuda-test:latest
        resources:
          limits:
            nvidia.com/gpu: 1
      restartPolicy: Never
  backoffLimit: 4
EOF

kubectl apply -f cuda-test-k8s.yaml
kubectl logs job/cuda-test-job
```

## Goal 5: Cloudflare Tunnel for Internet Access

### Step 5.1: Create Cloudflare Tunnel
1. Go to [Cloudflare Zero Trust Dashboard](https://dash.teams.cloudflare.com/)
2. Navigate to **Networks** → **Tunnels**
3. Create tunnel named `k8s-homelab`
4. Copy the tunnel token

### Step 5.2: Configure Tunnel Routes
In Cloudflare dashboard, add these routes:

| **Public Hostname** | **Service** |
|-------------------|------------|
| `go.yourdomain.com` | `http://go-hello-world-service:80` |
| `rust.yourdomain.com` | `http://rust-hello-world-service:80` |
| `llm.yourdomain.com` | `http://open-webui-service:80` |
| `traefik.yourdomain.com` | `http://traefik.traefik:80` |

### Step 5.3: Create Cloudflared Secret and Deployment
```bash
mkdir -p ~/k8s-apps/cloudflare-tunnel
cd ~/k8s-apps/cloudflare-tunnel

# Create secret file (consistent approach)
cat > tunnel-token.yaml << 'EOF'
apiVersion: v1
data:
    token: 'YOUR_BASE64_ENCODED_TOKEN_HERE'
kind: Secret
metadata:
    name: tunnel-credentials
    namespace: default
type: Opaque
EOF

# Replace with your actual token
echo -n "YOUR_ACTUAL_TUNNEL_TOKEN" | base64 -w 0
# Copy the output and replace YOUR_BASE64_ENCODED_TOKEN_HERE

# Create deployment
cat > cloudflared-deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: cloudflared
  name: cloudflared-deployment
  namespace: default
spec:
  replicas: 2
  selector:
    matchLabels:
      pod: cloudflared
  template:
    metadata:
      labels:
        pod: cloudflared
    spec:
      securityContext:
        sysctls:
        - name: net.ipv4.ping_group_range
          value: "65532 65532"
      containers:
      - command:
        - cloudflared
        - tunnel
        - --no-autoupdate
        - --metrics
        - 0.0.0.0:2000
        - run
        - --token
        - $(TUNNEL_TOKEN)
        env:
        - name: TUNNEL_TOKEN
          valueFrom:
            secretKeyRef:
              name: tunnel-credentials
              key: token
        image: cloudflare/cloudflared:latest
        name: cloudflared
        livenessProbe:
          httpGet:
            path: /ready
            port: 2000
          failureThreshold: 1
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 2000
          failureThreshold: 1
          initialDelaySeconds: 10
          periodSeconds: 10
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "200m"
EOF

# Deploy
kubectl apply -f tunnel-token.yaml
kubectl apply -f cloudflared-deployment.yaml

# Verify
kubectl get pods -l pod=cloudflared
kubectl logs -l pod=cloudflared
```

## Troubleshooting Guide

### Common Issues and Solutions

#### **Issue 1: MetalLB IP Not Accessible**
```bash
# Symptoms: Can't reach traefik external IP from other computers
# Solution: Reconfigure MetalLB with correct IP range

microk8s disable metallb
microk8s enable metallb:192.168.1.200-192.168.1.210

# Alternative: Use NodePort
kubectl patch svc traefik -n traefik -p '{"spec":{"type":"NodePort"}}'
kubectl get svc -n traefik  # Note the NodePort
```

#### **Issue 2: Pod Stuck in Pending**
```bash
# Check scheduling issues
kubectl describe pod POD_NAME

# Common causes:
# - hostNetwork port conflicts → Remove hostNetwork
# - Resource constraints → Reduce resource requests
# - Node taints → Add tolerations
```

#### **Issue 3: "Bad Gateway" from Traefik**
```bash
# Check service endpoints
kubectl get endpoints SERVICE_NAME

# Check pod logs
kubectl logs -l app=APP_NAME

# Check service selector matches pod labels
kubectl describe svc SERVICE_NAME | grep Selector
kubectl get pods --show-labels
```

#### **Issue 4: Open WebUI OOMKilled**
```bash
# Increase memory limits
kubectl patch deployment open-webui -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "open-webui",
            "resources": {
              "limits": {
                "memory": "2Gi"
              }
            }
          }
        ]
      }
    }
  }
}'
```

#### **Issue 5: Ollama Connection Refused**
```bash
# Configure Ollama to listen on all interfaces
sudo systemctl stop ollama
sudo mkdir -p /etc/systemd/system/ollama.service.d
cat | sudo tee /etc/systemd/system/ollama.service.d/override.conf << 'EOF'
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
EOF
sudo systemctl daemon-reload
sudo systemctl start ollama

# Test connectivity
curl http://$(hostname -I | awk '{print $1}'):11434/api/version
```

### Debugging Commands Reference

```bash
# General cluster health
kubectl get nodes
kubectl get pods -A
kubectl top nodes
kubectl top pods -A

# Service debugging
kubectl get svc -A
kubectl get endpoints
kubectl describe svc SERVICE_NAME

# Ingress debugging
kubectl get ingress -A
kubectl describe ingress INGRESS_NAME
kubectl logs -n traefik -l app.kubernetes.io/name=traefik

# Pod debugging
kubectl describe pod POD_NAME
kubectl logs POD_NAME -f
kubectl exec -it POD_NAME -- sh

# Network debugging
kubectl run debug --image=busybox --rm -it --restart=Never -- sh
# Inside debug pod: wget -O- http://SERVICE_NAME:PORT

# Resource usage
kubectl describe nodes | grep -A 5 "Allocated resources"
kubectl get events --sort-by='.lastTimestamp'
```

## Maintenance and Optimization

### Regular Cleanup
```bash
# Weekly cleanup script
#!/bin/bash
echo "🧹 Cleaning up Kubernetes cluster..."

# Clean Docker
docker image prune -f
docker system prune -f

# Clean failed pods
kubectl delete pods --field-selector=status.phase=Failed -A
kubectl delete pods --field-selector=status.phase=Succeeded -A

# Clean old replica sets
kubectl delete replicaset -A --field-selector='status.replicas=0'

# Clean logs
sudo journalctl --vacuum-time=7d

echo "✅ Cleanup complete!"
```

### Resource Optimization
```bash
# Scale down replicas if not needed
kubectl scale deployment go-hello-world --replicas=1
kubectl scale deployment rust-hello-world --replicas=1

# Reduce resource requests for development
kubectl patch deployment APP_NAME -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "CONTAINER_NAME",
            "resources": {
              "requests": {
                "memory": "64Mi",
                "cpu": "50m"
              }
            }
          }
        ]
      }
    }
  }
}'
```

### Monitoring Setup
```bash
# Enable metrics server for resource monitoring
microk8s enable metrics-server

# Check resource usage
kubectl top nodes
kubectl top pods -A

# Monitor specific deployments
watch kubectl get pods -l app=open-webui
```

## Access Summary

### Local Access (via Traefik)
- **Go App**: `http://go-hello.local`
- **Rust App**: `http://rust-hello.local`
- **Open WebUI**: `http://llm.local`
- **Traefik Dashboard**: `http://192.168.1.200/dashboard/`

### Internet Access (via Cloudflare)
- **Go App**: `https://go.yourdomain.com`
- **Rust App**: `https://rust.yourdomain.com`
- **Open WebUI**: `https://llm.yourdomain.com`
- **Traefik Dashboard**: `https://traefik.yourdomain.com/dashboard/`

### Network Access from Other Computers
```bash
# Add to /etc/hosts on other computers
echo "192.168.1.200 go-hello.local rust-hello.local llm.local" | sudo tee -a /etc/hosts

# Or use direct IP with Host header
curl -H "Host: llm.local" http://192.168.1.200
```

## Architecture Overview

```
🌍 Internet
    ↓ (Cloudflare Tunnel - HTTPS)
☁️ Cloudflare Edge
    ↓ 
🏠 cloudflared pods (in Kubernetes)
    ↓ (Internal cluster network)
    
🏠 Local Network (192.168.1.0/24)
    ↓
🎯 MetalLB LoadBalancer (192.168.1.200)
    ↓
🚦 Traefik Ingress Controller
    ↓ (Host-based routing)
├── 🐹 Go Hello World Service
├── 🦀 Rust Hello World Service  
├── 🤖 Open WebUI Service (LLM Interface)
└── 📊 Traefik Dashboard

🖥️ Host Services:
├── 🧠 Ollama (port 11434)
└── 🐳 Docker Registry (port 32000)

🎮 GPU Resources:
└── 🚀 CUDA Test Jobs (with NVIDIA GPU access)
```

## Goal 6: Secure SSH Access via Cloudflare Tunnel + Agentic AI

### Step 6.1: Configure Private Network Access in Cloudflare Tunnel

**Add your K8s server to the tunnel's private networks:**

1. **Go to Cloudflare Zero Trust Dashboard**:
   - Navigate to **Networks** → **Tunnels**
   - Click on your existing tunnel (`k8s-homelab`)

2. **Add Private Network**:
   - Go to **Private Networks** tab
   - Click **Add a private network**
   - **CIDR**: `192.168.1.0/24` (your home network)
   - **Comment**: `Home Lab Network`
   - Click **Save**

### Step 6.2: Create SSH Target in Cloudflare

**In Cloudflare Zero Trust Dashboard:**

1. **Go to Networks → Targets**
2. **Click "Add a target"**
3. **Configure Target**:
   - **Target hostname**: `k8s-server`
   - **IP addresses**: `192.168.1.253` (your K8s server IP)
   - **Virtual network**: Select your tunnel's virtual network
4. **Click "Add target"**

### Step 6.3: Create SSH Access Application

**In Cloudflare Zero Trust Dashboard:**

1. **Go to Access → Applications**
2. **Click "Add an application"**
3. **Select "Infrastructure"**
4. **Configure Application**:
   - **Application name**: `K8s Server SSH`
   - **Target criteria**:
     - **Target hostname**: `k8s-server`
     - **Protocol**: `SSH`
     - **Port**: `22`
5. **Click "Next"**

6. **Create Access Policy**:
   - **Policy name**: `Admin SSH Access`
   - **Action**: `Allow`
   - **Configure rule**: Add your email address
   - **Connection context**:
     - **SSH user**: `yoseph` (your username)
     - **☑️ Allow users to log in as their email alias**
7. **Click "Add application"**

### Step 6.4: Configure SSH Server to Trust Cloudflare CA

**On your K8s server (192.168.1.253):**

```bash
# Step 1: Get Cloudflare SSH CA public key
# You'll need to create an API token with "Access: SSH Auditing Edit" permission
# Then run this API call (replace with your account ID and API token):

# Generate SSH CA (if not already done)
curl --request POST \
  "https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/access/gateway_ca" \
  --header "Authorization: Bearer YOUR_API_TOKEN"

# Get the public key
curl https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/access/gateway_ca \
  --header "Authorization: Bearer YOUR_API_TOKEN"

# Step 2: Configure SSH server
cd /etc/ssh

# Create CA public key file (paste the public key from API response)
sudo vim ca.pub
# Paste the key like: ecdsa-sha2-nistp256 AAAAE2VjZHN... open-ssh-ca@cloudflareaccess.org

# Step 3: Update SSH configuration
sudo vim /etc/ssh/sshd_config

# Uncomment this line:
PubkeyAuthentication yes

# Add this line:
TrustedUserCAKeys /etc/ssh/ca.pub

# Step 4: Restart SSH service
sudo systemctl restart ssh

# Verify SSH is running
sudo systemctl status ssh
```

### Step 6.5: Install WARP Client on Your PC

**On your laptop/desktop:**

```bash
# Download and install WARP client
# For Ubuntu/Debian:
curl https://pkg.cloudflareclient.com/pubkey.gpg | sudo gpg --dearmor --output /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/cloudflare-client.list
sudo apt update
sudo apt install cloudflare-warp

# Register and connect
warp-cli register
warp-cli connect

# Check status
warp-cli status
```

### Step 6.6: Configure Split Tunnels

**In Cloudflare Zero Trust Dashboard:**

1. **Go to Settings → WARP Client**
2. **Go to Device settings → Profile settings**
3. **Click your device profile → Configure**
4. **Split Tunnels → Manage**
5. **Remove or modify RFC 1918 ranges**:
   - If in **Exclude mode**: Remove `192.168.0.0/16` and add specific ranges that exclude your network
   - If in **Include mode**: Add `192.168.1.0/24`

### Step 6.7: Test SSH Access

**From your laptop:**

```bash
# Test SSH connection through Cloudflare
ssh yoseph@192.168.1.253

# You should be prompted for Cloudflare authentication
# After authentication, you'll have SSH access to your K8s server

# Test kubectl access through SSH
ssh yoseph@192.168.1.253 'kubectl get nodes'
```

### Step 6.8: Install OpenCode on Your PC

```bash
# Install OpenCode on your laptop
curl -fsSL https://opencode.ai/install | bash

# Restart terminal or reload shell
source ~/.bashrc

# Verify installation
opencode --version
```

### Step 6.9: Configure OpenCode for Remote Access

**Option A: SSH Tunnel Method (Recommended)**

Create OpenCode config that uses SSH tunneling:

```bash
mkdir -p ~/.config/opencode

# Create configuration for remote access
cat > ~/.config/opencode/opencode.json << 'EOF'
{
  "providers": {
    "local-via-ssh": {
      "baseURL": "http://localhost:11434/v1",
      "apiKey": "not-required"
    }
  },
  "agents": {
    "k8s-agent": {
      "model": "local-via-ssh.codellama:7b",
      "provider": "local-via-ssh",
      "instructions": "You are a Kubernetes expert assistant with access to a remote cluster via SSH. You can help manage Kubernetes clusters, deploy applications, debug issues, and optimize configurations. The cluster has Traefik ingress, MetalLB load balancer, and runs Go/Rust applications plus Open WebUI for LLM access."
    },
    "coder": {
      "model": "local-via-ssh.qwen2.5-coder:7b", 
      "provider": "local-via-ssh",
      "instructions": "You are an expert software developer with access to a remote Kubernetes cluster via SSH. You can write, debug, and improve code, then deploy it to the cluster."
    }
  },
  "session": {
    "defaultAgent": "k8s-agent"
  }
}
EOF
```

**Option B: Direct Network Access**

```bash
# Alternative config for direct access (when WARP is connected)
cat > ~/.config/opencode/opencode-direct.json << 'EOF'
{
  "providers": {
    "remote-local": {
      "baseURL": "http://192.168.1.253:11434/v1",
      "apiKey": "not-required"
    }
  },
  "agents": {
    "k8s-agent": {
      "model": "remote-local.codellama:7b",
      "provider": "remote-local",
      "instructions": "You are a Kubernetes expert assistant with direct access to a remote cluster. You can help manage Kubernetes clusters, deploy applications, debug issues, and optimize configurations."
    }
  }
}
EOF
```

### Step 6.10: Create SSH-Aware Custom Commands

```bash
# Create commands directory
mkdir -p ~/.config/opencode/commands

# Create SSH tunnel management command
cat > ~/.config/opencode/commands/ssh-tunnel.md << 'EOF'
# Establish SSH Tunnel to K8s Server

Create SSH tunnels for secure access to remote services:

RUN ssh -f -N -L 11434:localhost:11434 yoseph@192.168.1.253
RUN ssh -f -N -L 8080:192.168.1.200:80 yoseph@192.168.1.253
RUN echo "Tunnels established:"
RUN echo "- Ollama LLM: http://localhost:11434"
RUN echo "- Traefik Dashboard: http://localhost:8080/dashboard/"
RUN ps aux | grep ssh | grep "11434\|8080"
EOF

# Create remote kubectl command
cat > ~/.config/opencode/commands/remote-kubectl.md << 'EOF'
# Remote Kubernetes Management

Execute kubectl commands on the remote K8s cluster via SSH:

RUN ssh yoseph@192.168.1.253 'kubectl get nodes -o wide'
RUN ssh yoseph@192.168.1.253 'kubectl get pods -A --sort-by=.metadata.creationTimestamp'
RUN ssh yoseph@192.168.1.253 'kubectl get svc -A | grep -E "(go-hello|rust-hello|open-webui|traefik)"'
RUN ssh yoseph@192.168.1.253 'kubectl get ingress'
RUN ssh yoseph@192.168.1.253 'kubectl top nodes'
EOF

# Create secure deployment command
cat > ~/.config/opencode/commands/secure-deploy.md << 'EOF'
# Secure Remote Deployment

Deploy application $APP_NAME securely via SSH:

RUN echo "Deploying $APP_NAME via secure SSH connection..."
RUN scp -r ./$APP_NAME yoseph@192.168.1.253:~/remote-deploy/
RUN ssh yoseph@192.168.1.253 "cd ~/remote-deploy/$APP_NAME && docker build -t localhost:32000/$APP_NAME:latest ."
RUN ssh yoseph@192.168.1.253 "docker push localhost:32000/$APP_NAME:latest"
RUN ssh yoseph@192.168.1.253 "kubectl set image deployment/$APP_NAME $APP_NAME=localhost:32000/$APP_NAME:latest"
RUN ssh yoseph@192.168.1.253 "kubectl rollout status deployment/$APP_NAME"
RUN ssh yoseph@192.168.1.253 "kubectl get pods -l app=$APP_NAME"
EOF

# Create comprehensive remote status command
cat > ~/.config/opencode/commands/remote-status.md << 'EOF'
# Comprehensive Remote Cluster Status

Get complete status of the remote Kubernetes cluster via secure SSH:

RUN echo "=== Cluster Connectivity ==="
RUN ssh yoseph@192.168.1.253 'kubectl cluster-info'

RUN echo "=== Node Status ==="
RUN ssh yoseph@192.168.1.253 'kubectl get nodes -o wide'
RUN ssh yoseph@192.168.1.253 'kubectl top nodes'

RUN echo "=== Application Status ==="
RUN ssh yoseph@192.168.1.253 'kubectl get pods -l app=go-hello-world'
RUN ssh yoseph@192.168.1.253 'kubectl get pods -l app=rust-hello-world'
RUN ssh yoseph@192.168.1.253 'kubectl get pods -l app=open-webui'

RUN echo "=== Service Endpoints ==="
RUN ssh yoseph@192.168.1.253 'kubectl get svc -A | grep -E "(go-hello|rust-hello|open-webui|traefik)"'
RUN ssh yoseph@192.168.1.253 'kubectl get ingress'

RUN echo "=== Application Health ==="
RUN ssh yoseph@192.168.1.253 'curl -s http://go-hello.local | head -1'
RUN ssh yoseph@192.168.1.253 'curl -s http://rust-hello.local | head -1'
RUN ssh yoseph@192.168.1.253 'curl -s http://llm.local | grep -o "<title>[^<]*"'
EOF
```

### Step 6.11: Usage Examples

**Start SSH tunnel and use OpenCode:**

```bash
# Method 1: Manual SSH tunnel
ssh -f -N -L 11434:localhost:11434 yoseph@192.168.1.253
opencode "What's the current status of my Kubernetes cluster?"

# Method 2: Use custom commands
opencode user:ssh-tunnel
opencode user:remote-status
opencode user:remote-kubectl

# Method 3: Complex remote operations
opencode "Help me debug why my Open WebUI pod was restarting and fix any issues"

# Method 4: Secure deployment
opencode user:secure-deploy APP_NAME=myapp
```

**Advanced agentic workflows:**

```bash
# Comprehensive cluster management
opencode "Connect to my remote cluster, analyze all running services, identify any performance issues, and suggest optimizations"

# Secure development workflow
opencode "I have a new Node.js app in ./my-node-app. Deploy it securely to my remote cluster with proper ingress configuration"

# Remote troubleshooting
opencode "My rust-hello.local is returning 502 errors. Investigate the issue remotely and fix it"
```

### Step 6.12: Security Benefits

**Your setup now provides:**

✅ **Zero-trust access**: SSH through Cloudflare with short-lived certificates  
✅ **No exposed ports**: SSH not directly accessible from internet  
✅ **Audit logging**: All SSH commands logged and encrypted  
✅ **Device policy**: Only enrolled devices can access  
✅ **Conditional access**: Based on user, device, location  
✅ **Secure tunneling**: Encrypted channels for all communications  

### Step 6.13: Mobile Access (Bonus)

**Install WARP on mobile devices for SSH access:**

1. Install **Cloudflare WARP** app
2. **Enroll device** in your Zero Trust organization
3. **Use SSH apps** like Termius to connect to `192.168.1.253`
4. **Full cluster access** from anywhere!

This completes your **secure agentic AI integration**! Now you can:
- 🔒 **Securely SSH** to your K8s server from anywhere
- 🤖 **Run OpenCode** with full cluster access
- 📱 **Access from any device** with WARP
- 🔐 **Zero-trust security** with audit logging
- 🌍 **Global access** to your home lab

### Step 6.3: Configure OpenCode for Your Setup

Create OpenCode configuration:

```bash
# Create config directory
mkdir -p ~/.config/opencode

# Create configuration file
cat > ~/.config/opencode/opencode.json << 'EOF'
{
  "providers": {
    "openai": {
      "apiKey": "YOUR_OPENAI_KEY_HERE"
    },
    "local": {
      "baseURL": "http://192.168.1.253:11434/v1",
      "apiKey": "not-required"
    }
  },
  "agents": {
    "k8s-agent": {
      "model": "local.codellama:7b",
      "provider": "local",
      "instructions": "You are a Kubernetes expert assistant. You have access to kubectl and can help manage Kubernetes clusters, deploy applications, debug issues, and optimize configurations."
    },
    "coder": {
      "model": "local.qwen2.5-coder:7b",
      "provider": "local",
      "instructions": "You are an expert software developer. You can write, debug, and improve code in any language. You have access to the Kubernetes cluster and can deploy applications."
    }
  },
  "session": {
    "defaultAgent": "k8s-agent"
  }
}
EOF

# Replace with your server IP
SERVER_IP="192.168.1.253"  # Your K8s server IP
sed -i "s/192.168.1.253/$SERVER_IP/g" ~/.config/opencode/opencode.json
```

### Step 6.4: Create Custom Kubernetes Commands

Create custom commands for common K8s operations:

```bash
# Create commands directory
mkdir -p ~/.config/opencode/commands

# K8s status command
cat > ~/.config/opencode/commands/k8s-status.md << 'EOF'
# Kubernetes Cluster Status

Get comprehensive status of the Kubernetes cluster including:
- Node status and resource usage
- Running pods across all namespaces
- Service endpoints and ingress status
- Recent events and any issues

RUN kubectl get nodes -o wide
RUN kubectl top nodes
RUN kubectl get pods -A --sort-by='.metadata.creationTimestamp'
RUN kubectl get svc -A
RUN kubectl get ingress -A
RUN kubectl get events --sort-by='.lastTimestamp' | tail -10
EOF

# Deploy application command
cat > ~/.config/opencode/commands/k8s-deploy.md << 'EOF'
# Deploy Application to Kubernetes

Deploy a new application with name $APP_NAME using image $IMAGE:

1. Create deployment, service, and ingress
2. Monitor rollout status
3. Verify endpoints and accessibility

RUN kubectl create deployment $APP_NAME --image=$IMAGE
RUN kubectl expose deployment $APP_NAME --port=80 --target-port=8080
RUN kubectl create ingress $APP_NAME-ingress --rule="$APP_NAME.local/*=$APP_NAME:80" --class=traefik
RUN kubectl rollout status deployment/$APP_NAME
RUN kubectl get endpoints $APP_NAME
EOF

# Debug pod command
cat > ~/.config/opencode/commands/k8s-debug.md << 'EOF'
# Debug Kubernetes Pod Issues

Debug pod $POD_NAME in namespace $NAMESPACE (default if not specified):

RUN kubectl describe pod $POD_NAME ${NAMESPACE:+-n $NAMESPACE}
RUN kubectl logs $POD_NAME ${NAMESPACE:+-n $NAMESPACE} --tail=50
RUN kubectl get events --field-selector involvedObject.name=$POD_NAME ${NAMESPACE:+-n $NAMESPACE}
EOF

# Scale application command
cat > ~/.config/opencode/commands/k8s-scale.md << 'EOF'
# Scale Kubernetes Application

Scale deployment $DEPLOYMENT to $REPLICAS replicas:

RUN kubectl scale deployment $DEPLOYMENT --replicas=$REPLICAS
RUN kubectl rollout status deployment/$DEPLOYMENT
RUN kubectl get pods -l app=$DEPLOYMENT
EOF

# Resource monitoring command
cat > ~/.config/opencode/commands/k8s-monitor.md << 'EOF'
# Monitor Kubernetes Resources

Monitor resource usage and performance:

RUN kubectl top nodes
RUN kubectl top pods -A --sort-by=memory
RUN kubectl describe nodes | grep -A 5 "Allocated resources"
RUN docker system df
EOF
```

### Step 6.5: Create Project-Specific Agent Commands

For your specific setup, create targeted commands:

```bash
# Open WebUI management
cat > ~/.config/opencode/commands/manage-openwebui.md << 'EOF'
# Manage Open WebUI LLM Interface

Check and manage the Open WebUI deployment:

RUN kubectl get pods -l app=open-webui
RUN kubectl logs -l app=open-webui --tail=20
RUN kubectl get svc open-webui-service
RUN kubectl get ingress open-webui-ingress
RUN curl -s http://llm.local | grep -o "<title>[^<]*" | head -1
EOF

# Application status command
cat > ~/.config/opencode/commands/check-apps.md << 'EOF'
# Check All Applications Status

Verify all deployed applications are healthy:

RUN echo "=== Go Hello World ==="
RUN kubectl get pods -l app=go-hello-world
RUN curl -s http://go-hello.local | head -2

RUN echo "=== Rust Hello World ==="
RUN kubectl get pods -l app=rust-hello-world
RUN curl -s http://rust-hello.local

RUN echo "=== Open WebUI ==="
RUN kubectl get pods -l app=open-webui
RUN curl -s http://llm.local | grep -o "<title>[^<]*"

RUN echo "=== Traefik Status ==="
RUN kubectl get svc -n traefik
RUN curl -s http://192.168.1.200/dashboard/ | grep -o "<title>[^<]*"
EOF

# Development workflow command
cat > ~/.config/opencode/commands/dev-workflow.md << 'EOF'
# Development Workflow

Complete development workflow for application $APP_NAME:

1. Build and push Docker image
2. Deploy to Kubernetes
3. Create ingress
4. Test deployment

CONTEXT: Working on application $APP_NAME in directory ./$APP_NAME

RUN cd $APP_NAME && docker build -t localhost:32000/$APP_NAME:latest .
RUN docker push localhost:32000/$APP_NAME:latest
RUN kubectl set image deployment/$APP_NAME $APP_NAME=localhost:32000/$APP_NAME:latest
RUN kubectl rollout status deployment/$APP_NAME
RUN kubectl get pods -l app=$APP_NAME
RUN curl -s http://$APP_NAME.local
EOF
```

### Step 6.6: Start Using OpenCode

**Basic usage:**

```bash
# Start OpenCode with your K8s agent
opencode

# Or start with specific agent
opencode --agent k8s-agent

# Use custom commands
opencode user:k8s-status
opencode user:check-apps
opencode user:manage-openwebui

# Interactive session for complex tasks
opencode "Help me deploy a new Python Flask application to my Kubernetes cluster"
```

### Step 6.7: Advanced Agentic Workflows

**Create complex multi-step workflows:**

```bash
# Create a comprehensive deployment agent
cat > ~/.config/opencode/commands/full-deployment.md << 'EOF'
# Full Application Deployment Pipeline

Deploy application $APP_NAME from GitHub repo $REPO_URL:

1. Clone repository
2. Build Docker image  
3. Push to local registry
4. Deploy to Kubernetes
5. Create service and ingress
6. Monitor deployment
7. Test accessibility

RUN git clone $REPO_URL $APP_NAME-deploy
RUN cd $APP_NAME-deploy && docker build -t localhost:32000/$APP_NAME:latest .
RUN docker push localhost:32000/$APP_NAME:latest
RUN kubectl create deployment $APP_NAME --image=localhost:32000/$APP_NAME:latest
RUN kubectl expose deployment $APP_NAME --port=80 --target-port=8080
RUN kubectl create ingress $APP_NAME-ingress --rule="$APP_NAME.local/*=$APP_NAME:80" --class=traefik
RUN sleep 30
RUN kubectl rollout status deployment/$APP_NAME
RUN kubectl get all -l app=$APP_NAME
RUN curl -s http://$APP_NAME.local
RUN echo "Add to /etc/hosts: 192.168.1.200 $APP_NAME.local"
EOF

# Create monitoring and alerting agent
cat > ~/.config/opencode/commands/cluster-health.md << 'EOF'
# Comprehensive Cluster Health Check

Perform complete health assessment of the Kubernetes cluster:

RUN echo "=== Node Health ==="
RUN kubectl get nodes -o wide
RUN kubectl describe nodes | grep -E "(Conditions|Capacity|Allocatable)" -A 3

RUN echo "=== Resource Usage ==="
RUN kubectl top nodes
RUN kubectl top pods -A --sort-by=cpu | head -10

RUN echo "=== Storage Status ==="
RUN kubectl get pv
RUN kubectl get pvc -A

RUN echo "=== Network Status ==="
RUN kubectl get svc -A | grep -E "(LoadBalancer|NodePort)"
RUN kubectl get ingress -A

RUN echo "=== Recent Events ==="
RUN kubectl get events --sort-by='.lastTimestamp' | tail -15

RUN echo "=== Failed Pods ==="
RUN kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded

RUN echo "=== Application Health ==="
RUN curl -s http://go-hello.local | head -1
RUN curl -s http://rust-hello.local | head -1  
RUN curl -s http://llm.local | grep -o "<title>[^<]*"
EOF
```

### Step 6.8: Remote Development Capabilities

**Set up OpenCode for remote development:**

```bash
# Create remote development command
cat > ~/.config/opencode/commands/remote-dev.md << 'EOF'
# Remote Development Session

Start a development session with access to the remote Kubernetes cluster:

1. Check cluster connectivity
2. Set up development environment
3. Enable file watching for auto-deployment

RUN kubectl cluster-info
RUN kubectl get namespaces
RUN kubectl config current-context
RUN echo "Remote development environment ready!"
RUN echo "Available commands: k8s-status, check-apps, dev-workflow, full-deployment"
EOF

# Enable continuous deployment
cat > ~/.config/opencode/commands/watch-deploy.md << 'EOF'
# Continuous Deployment Watcher

Watch for changes in application $APP_NAME and auto-deploy:

RUN echo "Setting up continuous deployment for $APP_NAME"
RUN cd $APP_NAME
RUN while inotifywait -e modify -r .; do
    echo "Changes detected, rebuilding and deploying..."
    docker build -t localhost:32000/$APP_NAME:latest .
    docker push localhost:32000/$APP_NAME:latest
    kubectl set image deployment/$APP_NAME $APP_NAME=localhost:32000/$APP_NAME:latest
    kubectl rollout status deployment/$APP_NAME
    echo "Deployment complete!"
done
EOF
```

### Step 6.9: Integration with Your Existing Setup

**Connect OpenCode to your Open WebUI models:**

```bash
# Test connection to your LLM setup
opencode "Connect to my local LLM setup and list available models"

# Use different models for different tasks
cat > ~/.config/opencode/agents.json << 'EOF'
{
  "k8s-expert": {
    "model": "local.codellama:7b",
    "provider": "local",
    "instructions": "Kubernetes expert specializing in cluster management, troubleshooting, and optimization."
  },
  "code-reviewer": {
    "model": "local.qwen2.5-coder:7b", 
    "provider": "local",
    "instructions": "Senior software engineer focused on code review, best practices, and security."
  },
  "devops-engineer": {
    "model": "local.codellama:7b",
    "provider": "local", 
    "instructions": "DevOps specialist for CI/CD, containerization, and infrastructure automation."
  }
}
EOF
```

### Step 6.10: Security Considerations

**Secure your agentic AI setup:**

```bash
# Create restricted kubectl config for OpenCode
kubectl config view --raw > ~/.kube/config-opencode

# Create role-based access (optional)
cat > opencode-rbac.yaml << 'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: opencode-agent
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: opencode-agent
rules:
- apiGroups: [""]
  resources: ["pods", "services", "endpoints"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: opencode-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: opencode-agent
subjects:
- kind: ServiceAccount
  name: opencode-agent
  namespace: default
EOF

kubectl apply -f opencode-rbac.yaml
```

This completes your **Agentic AI Integration** setup! 🤖 Now you have:

- ✅ **Terminal-based AI agent** on your PC
- ✅ **Direct access** to your Kubernetes cluster  
- ✅ **Custom commands** for common operations
- ✅ **Integration** with your Open WebUI/DeepSeek models
- ✅ **Multi-agent workflows** for different tasks
- ✅ **Remote development** capabilities
- ✅ **Secure access** patterns

**Usage Examples:**
```bash
# Start agentic session
opencode "Help me deploy a new Node.js application to my cluster"

# Use custom commands  
opencode user:k8s-status
opencode user:check-apps
opencode user:full-deployment APP_NAME=myapp REPO_URL=https://github.com/user/repo

# Multi-step workflows
opencode "Analyze my cluster performance, identify bottlenecks, and suggest optimizations"
```
