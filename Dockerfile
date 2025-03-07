# 使用官方 Go 镜像
FROM golang:1.24.1

# 设置工作目录
WORKDIR /app

# 复制所有文件
COPY . .

# 下载依赖
RUN go mod tidy

# 编译 Go 代码
RUN go build -o server

# 运行服务器
CMD ["/app/server"]