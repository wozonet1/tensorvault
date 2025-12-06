import io
from typing import Iterator


class TensorVaultReader(io.RawIOBase):
    """
    A file-like object that wraps a gRPC download stream.

    Allows libraries like Pandas, PIL, or Tarfile to read directly from
    the network stream as if it were a local file.
    """

    def __init__(self, stream_iterator: Iterator):
        """
        Args:
            stream_iterator: The gRPC response iterator (yields DownloadResponse).
        """
        self._iter = stream_iterator
        self._buffer = b""  # 内部蓄水池 (Internal Buffer)
        self._position = 0  # 当前读取位置 (用于 tell)
        self._closed = False
        self._depleted = False  # 标记流是否已耗尽

    def readable(self) -> bool:
        """告诉调用者：我是可读的"""
        return True

    def seekable(self) -> bool:
        """
        gRPC 流通常不支持随机 seek (回退)。
        如果用户尝试 seek，我们诚实地返回 False。
        (Pandas read_csv 不需要 seek，但有些库可能需要)
        """
        return False

    def read(self, size: int = -1) -> bytes:
        """
        核心方法：读取指定大小的数据。

        Args:
            size: 要读取的字节数。-1 表示读取全部。
        """
        if self._closed:
            raise ValueError("I/O operation on closed file.")

        # Case 1: 读取全部 (Read All)
        # 这是一个内存敏感的操作，但在 Python Client 中通常是可以接受的
        if size == -1:
            return self._read_all()

        # Case 2: 读取特定长度 (Buffered Read)
        return self._read_exact(size)

    def _read_all(self) -> bytes:
        """消耗迭代器中的所有剩余数据。"""
        if self._depleted:
            result = self._buffer
            self._buffer = b""
            return result

        # 收集所有剩余的 chunks
        chunks = [self._buffer]
        try:
            for response in self._iter:
                chunks.append(response.chunk_data)
        except Exception:
            # 网络中断等异常
            raise

        self._depleted = True
        self._buffer = b""

        result = b"".join(chunks)
        self._position += len(result)
        return result

    def _read_exact(self, size: int) -> bytes:
        """尝试读取 size 个字节。"""
        # 1. 填充缓冲区，直到够用，或者流断了
        while len(self._buffer) < size and not self._depleted:
            try:
                # [Pull] 从 gRPC 迭代器拉取下一个包
                response = next(self._iter)
                # [Push] 存入蓄水池
                self._buffer += response.chunk_data
            except StopIteration:
                self._depleted = True
                break
            except Exception as e:
                # 处理 gRPC 异常 (如连接中断)
                # 这里可以包一层 TensorVault 的异常，或者直接抛出
                raise IOError(f"Stream interrupted: {e}") from e

        # 2. 从缓冲区切片返回
        # 如果 buffer 长度小于 size (且流耗尽)，就返回剩下的所有 (EOF 行为)
        read_size = min(len(self._buffer), size)

        data = self._buffer[:read_size]
        self._buffer = self._buffer[read_size:]  # 移除已读部分

        self._position += len(data)
        return data

    def close(self):
        """关闭流资源"""
        if not self._closed:
            self._closed = True
            # 对于 gRPC 客户端流，通常停止迭代就会通知服务端取消（取决于具体实现）
            # 我们主动丢弃迭代器引用
            self._iter = None
            self._buffer = b""
            super().close()
