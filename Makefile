.PHONY: service install clean

all: service

service:
	@echo "⚡ 正在编译..."
	go build -o bin/tinyclue cmd/main.go
	@echo "🎉 编译成功！"

install:
	@echo "⚡ 安装中..."
	./install.sh

clean:
	@echo "🧹 清理中..."
	rm -rf bin/
	@echo "✅ 清理完成！"