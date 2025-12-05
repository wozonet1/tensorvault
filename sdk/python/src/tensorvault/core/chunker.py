from typing import BinaryIO, Generator

# 64KB 是 gRPC 和 HTTP/2 协议推荐的 Payload Frame 大小
# 过大可能导致 Head-of-Line Blocking，过小会导致帧头开销过大
DEFAULT_CHUNK_SIZE = 64 * 1024


def read_in_chunks(
    file_obj: BinaryIO, chunk_size: int = DEFAULT_CHUNK_SIZE
) -> Generator[bytes, None, None]:
    """
    生成器：按固定大小读取文件流。

    架构说明：
    这是 "Network Chunking" (网络分片)，用于 gRPC 流式传输。
    它不同于服务端的 "Storage Chunking" (FastCDC)。
    仅仅是将文件切分为多个小块进行传输，以适应网络协议和缓冲区。

    Args:
        file_obj: 已打开的文件对象 (mode='rb')
        chunk_size: 每次读取的字节数，默认 64KB

    Yields:
        bytes: 文件数据块
    """
    while True:
        data = file_obj.read(chunk_size)
        if not data:
            break
        yield data
