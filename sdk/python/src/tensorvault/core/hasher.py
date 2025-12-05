import hashlib
import os

# 1MB 的读取缓冲区，对于现代 SSD 和 OS 文件缓存来说是比较高效的平衡点
BUFFER_SIZE = 1024 * 1024


def calculate_linear_sha256(file_path: str) -> str:
    """
    计算文件的全量线性 SHA-256 哈希 (Standard SHA-256).

    注意：
    1. 这不是 Merkle Root，主要用于 `CheckFile` (秒传) 和端到端完整性校验。
    2. 使用分块读取，内存占用恒定，可以安全处理 10GB+ 大文件。

    Args:
        file_path: 本地文件路径

    Returns:
        64位 Hex 字符串
    """
    if not os.path.exists(file_path):
        raise FileNotFoundError(f"File not found: {file_path}")

    sha256 = hashlib.sha256()

    with open(file_path, "rb") as f:
        while True:
            data = f.read(BUFFER_SIZE)
            if not data:
                break
            sha256.update(data)

    return sha256.hexdigest()
