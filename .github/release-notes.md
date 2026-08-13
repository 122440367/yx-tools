## 这版改了什么

这版完善了远程 IP 列表和 GitHub 上报能力：

- `-f` 和网页端 IP 文件现在都支持 HTTP(S) 链接，可直接读取远程 IP、CIDR 与 `IP:端口` 列表。
- GitHub 上报格式更简洁，只输出可直接使用的节点信息，并修正网络厂商识别格式。
- 补充了不限速度、延迟以及候选 IP 数量等常用测速示例。

## 下载哪个文件

| 你的设备 | 下载 |
| --- | --- |
| Windows（绝大多数电脑） | `yx_windows_amd64.zip` |
| Windows（骁龙/ARM 笔记本） | `yx_windows_arm64.zip` |
| Mac（M1/M2/M3/M4 芯片） | `yx_darwin_arm64.tar.gz` |
| Mac（2020 年前的 Intel 机型） | `yx_darwin_amd64.tar.gz` |
| Linux 服务器（常见的 x86_64） | `yx_linux_amd64.tar.gz` |
| Linux（ARM，如甲骨文免费机、树莓派） | `yx_linux_arm64.tar.gz` |
| Linux（很老的 32 位机器） | `yx_linux_386.tar.gz` |
| FreeBSD | `yx_freebsd_amd64.tar.gz` |

不确定 Mac 是哪种芯片：左上角苹果图标 →「关于本机」，写着 Apple M 开头的选 arm64，写 Intel 的选 amd64。

不确定 Linux 是哪种：命令行执行 `uname -m`，显示 `x86_64` 选 amd64，显示 `aarch64` 选 arm64。

## 怎么运行

Linux / Mac：

```bash
tar -xzf yx_linux_amd64.tar.gz
chmod +x yx_linux_amd64
./yx_linux_amd64 web
```

解压后的文件名带平台后缀。`web` 会启动图形界面，并自动打开 <http://127.0.0.1:8080>。在服务器上运行、需要从其他机器访问时：

```bash
./yx_linux_amd64 web -listen 0.0.0.0:8080 -no-open
```

Mac 首次打开若提示“无法验证开发者”：进入「系统设置 → 隐私与安全性」，在页面底部选择“仍要打开”。

Windows 解压后可直接双击运行；也可以在文件夹中打开 PowerShell：

```powershell
.\yx_windows_amd64.exe web
```

Docker：

```bash
docker run -d --name yx -p 8080:8080 -v $(pwd)/data:/data ghcr.io/byjoey/yx-tools:latest
```

## 校验文件完整性

`checksums.txt` 包含全部 8 个压缩包的 SHA256：

```bash
sha256sum -c checksums.txt
```

macOS 可以使用 `shasum -a 256 -c checksums.txt`。
