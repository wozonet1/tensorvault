import os
from typing import Any, Dict, Generator, cast

import grpc

from tensorvault.api.index import Index
from tensorvault.api.io import TensorVaultReader
from tensorvault.core import chunker, hasher
from tensorvault.grpc.stub_manager import StubManager
from tensorvault.utils import errors
from tensorvault.v1 import tensorvault_pb2


class Client:
    def __init__(self, addr: str = "localhost:8080"):
        self._stubs = StubManager(addr)

    def close(self):
        self._stubs.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()

    # --- Meta Operations ---

    def get_head(self) -> Dict[str, Any]:
        """获取当前仓库的 HEAD 状态。"""
        req = tensorvault_pb2.GetHeadRequest()
        try:
            resp = self._stubs.meta.GetHead(req)
            return {"exists": resp.exists, "hash": resp.hash, "version": resp.version}
        except grpc.RpcError as e:
            self._handle_grpc_error(e)
            return {}  # Should not reach here

    def new_index(self) -> Index:
        """创建一个新的内存暂存区 (Index) 用于批量提交文件。"""
        return Index(self)

    # [新增] 提交接口
    def commit(
        self,
        tree_hash: str,
        message: str,
        branch: str = "HEAD",
        author: str = "PythonSDK",
    ) -> str:
        """
        创建一个 Commit。
        自动获取当前 Branch 的 HEAD 作为父节点 (Linear History)。
        """
        # 1. 尝试获取当前 HEAD 作为 Parent
        parent_hashes = []
        # [修正] 不要用 try-except 包裹整个逻辑
        # get_head 内部已经处理了 grpc 错误并抛出 TensorVaultError
        # 如果是网络错误，让它抛出，中止提交，保护数据一致性
        head_info = self.get_head()

        # 只有在明确 "存在 HEAD" 时才添加父节点
        # 如果 exists=False (Initial Commit)，自然跳过，逻辑是安全的
        if head_info.get("exists") and head_info.get("hash"):
            parent_hashes.append(head_info["hash"])

        req = tensorvault_pb2.CommitRequest(
            message=message,
            author=author,
            tree_hash=tree_hash,
            branch_name=branch,
            parent_hashes=parent_hashes,
        )

        try:
            resp = self._stubs.meta.Commit(req)
            return cast(str, resp.commit_hash)
        except grpc.RpcError as e:
            self._handle_grpc_error(e)
            return ""

    # [新增] 内部方法：构建树
    def _build_tree(self, file_map: Dict[str, str]) -> str:
        """
        调用服务端的 BuildTree RPC。
        Args:
            file_map: { "path/to/file": "hash" }
        """
        req = tensorvault_pb2.BuildTreeRequest(file_map=file_map)
        try:
            resp = self._stubs.meta.BuildTree(req)
            return cast(str, resp.tree_hash)
        except grpc.RpcError as e:
            self._handle_grpc_error(e)
            return ""

    # --- Data Operations ---

    def upload(self, file_path: str) -> str:
        """
        上传文件到 TensorVault (双阶段上传策略)。

        流程:
        1. 计算本地文件的全量线性哈希 (SHA-256)。
        2. CheckFile: 询问服务端是否已存在 (Optimistic Dedup)。
        3. 如果存在 -> 秒传成功，直接返回 Merkle Root。
        4. 如果不存在 -> 启动流式上传 (Client-Side Streaming)。

        Args:
            file_path: 本地文件路径

        Returns:
            str: 文件的 Merkle Root Hash
        """
        if not os.path.exists(file_path):
            raise FileNotFoundError(f"File not found: {file_path}")

        file_size = os.path.getsize(file_path)

        # --- Phase 1: 计算指纹 (Fingerprinting) ---
        # 这是一个本地 IO 操作
        linear_sha256 = hasher.calculate_linear_sha256(file_path)

        try:
            # --- Phase 2: 预检查 (Pre-check) ---
            check_req = tensorvault_pb2.CheckFileRequest(
                sha256=linear_sha256, size=file_size
            )
            check_resp = self._stubs.data.CheckFile(check_req)

            # [Branch A] 秒传命中
            if check_resp.exists:
                # 注意：proto3 optional 字段在 python 中默认是 None 或具体值
                # 但 check_resp.merkle_root_hash 是直接访问属性
                # 如果没设置，它会是空字符串吗？
                # 在 Python Protobuf 中，HasField 检查 optional 字段
                if check_resp.HasField("merkle_root_hash"):
                    return cast(str, check_resp.merkle_root_hash)
                # 防御性逻辑：如果 Server 说存在但没给 Hash (不应发生)
                raise errors.ServerError(
                    "Server indicated existence but returned no hash."
                )

            # [Branch B] 流式上传
            return self._perform_streaming_upload(file_path, linear_sha256)

        except grpc.RpcError as e:
            self._handle_grpc_error(e)
            return ""  # Should not reach here

    def open(self, hash_str: str) -> TensorVaultReader:
        """
        打开一个远程文件流。

        Args:
            hash_str: 文件的 Merkle Root Hash。

        Returns:
            一个 file-like object，可以直接传给 pandas.read_csv() 等。
        """
        # 1. 构造请求
        req = tensorvault_pb2.DownloadRequest(hash=hash_str)

        # 2. 获取 gRPC 迭代器 (注意：此时还没有开始下载数据，Lazy execution)
        try:
            stream_iterator = self._stubs.data.Download(req)
        except grpc.RpcError as e:
            self._handle_grpc_error(e)

        # 3. 包装成 IO 对象并返回
        return TensorVaultReader(stream_iterator)

    def _perform_streaming_upload(self, file_path: str, sha256: str) -> str:
        """执行实际的流式上传 (内部方法)。"""

        # 构造请求生成器
        def request_iterator() -> Generator[tensorvault_pb2.UploadRequest, None, None]:
            # Frame 1: Metadata (必须包含 sha256 用于校验)
            yield tensorvault_pb2.UploadRequest(
                meta=tensorvault_pb2.FileMeta(
                    path=os.path.basename(file_path), sha256=sha256
                )
            )

            # Frame 2...N: Binary Chunks
            # 使用 chunker 按 64KB 读取
            with open(file_path, "rb") as f:
                for chunk in chunker.read_in_chunks(f):
                    yield tensorvault_pb2.UploadRequest(chunk_data=chunk)

        # 发送请求 (阻塞等待直到结束)
        response = self._stubs.data.Upload(request_iterator())
        return cast(str, response.hash)

    # --- Error Handling Helper ---

    def _handle_grpc_error(self, e: grpc.RpcError):
        """将 gRPC 状态码转换为 Python 异常。"""
        code = e.code()
        details = e.details()

        if code == grpc.StatusCode.UNAVAILABLE:
            raise errors.NetworkError(f"Could not connect to server: {details}") from e
        elif code == grpc.StatusCode.DATA_LOSS:
            raise errors.IntegrityError(f"Data corruption detected: {details}") from e
        elif code == grpc.StatusCode.INTERNAL:
            raise errors.ServerError(f"Server internal error: {details}") from e
        else:
            raise errors.TensorVaultError(f"RPC failed [{code}]: {details}") from e
