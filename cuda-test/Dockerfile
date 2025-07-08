FROM nvidia/cuda:12.9.1-cudnn-devel-ubuntu24.04

RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY cuda-test.cu .
RUN nvcc -o cuda-test cuda-test.cu

CMD ["./cuda-test"]
