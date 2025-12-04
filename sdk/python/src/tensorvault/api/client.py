from tensorvault.grpc.stub_manager import StubManager
from tensorvault.v1 import tensorvault_pb2


class Client:
    """
    TensorVault Python Client.

    Usage:
        client = Client("localhost:8080")
        head = client.get_head()
    """

    def __init__(self, addr: str = "localhost:8080"):
        self._stubs = StubManager(addr)

    def close(self):
        self._stubs.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

    # --- Meta Operations ---

    def get_head(self) -> dict:
        """
        获取当前仓库的 HEAD 状态。
        """
        req = tensorvault_pb2.GetHeadRequest()

        # 调用 gRPC
        resp = self._stubs.meta.GetHead(req)

        # 转换为 Python 原生字典返回，对用户屏蔽 Protobuf 对象
        return {"exists": resp.exists, "hash": resp.hash, "version": resp.version}
