# sudo apt-get docker-ce docker-ce-cli containerd.io install binfmt-support qemu-user-static
# docker buildx create --use --name cross-platform-build
# docker buildx inspect --bootstrap cross-platform-build
docker buildx build -f Dockerfile --platform linux/amd64,linux/arm64 -t visago/jumpgate:$(git describe) --no-cache --push .
docker buildx build -f Dockerfile --platform linux/amd64,linux/arm64 -t visago/jumpgate:latest --no-cache --push .
