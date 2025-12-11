import logging
import os

# 为了类型提示，引用 Client 但不造成循环导入 (运行时不导入)
from typing import TYPE_CHECKING, Dict, Optional

if TYPE_CHECKING:
    from tensorvault.api.client import Client

logger = logging.getLogger(__name__)


class Index:
    """
    内存暂存区 (In-Memory Staging Area)。
    用于收集一次实验或事务中产生的所有文件，并打包提交。
    """

    def __init__(self, client: "Client"):
        self._client = client
        # Map[remote_path, merkle_root_hash]
        self._entries: Dict[str, str] = {}

    def add(self, local_path: str, remote_path: Optional[str] = None) -> str:
        """
        添加文件到暂存区（会自动触发上传）。

        Args:
            local_path: 本地文件路径。
            remote_path: 在 TensorVault 仓库中的路径 (Key)。
                         如果不填，默认使用 local_path 相对于当前工作目录的路径。

        Returns:
            str: 文件的 Hash。
        """
        if not os.path.exists(local_path):
            raise FileNotFoundError(f"File not found: {local_path}")

        # 1. 自动计算远程路径 (保持目录结构)
        if remote_path is None:
            # 默认保留相对路径，例如 "processed/fold_1/graph.csv"
            # 这样在 TV 里重建出来的树也是这个结构
            remote_path = os.path.relpath(local_path, os.getcwd())
            # Windows 兼容性修正：强制使用 '/' 作为路径分隔符
            remote_path = remote_path.replace(os.sep, "/")

        logger.info(f"➕ [Index] Adding: {remote_path} <- {local_path}")

        # 2. 上传文件 (利用 Client 的秒传逻辑)
        file_hash = self._client.upload(local_path)

        # 3. 记录到暂存区
        self._entries[remote_path] = file_hash

        return file_hash

    def commit(
        self, message: str, branch: str = "HEAD", author: str = "PythonSDK"
    ) -> str:
        """
        将暂存区的所有内容打包为一个 Commit。

        流程:
        1. 发送 _entries 给服务端 BuildTree -> 得到 TreeHash。
        2. 发送 Commit 请求 (包含 TreeHash 和 Parent) -> 得到 CommitHash。
        """
        if not self._entries:
            logger.warning("⚠️ [Index] Nothing to commit (staging area is empty).")
            return ""

        logger.info(f"📦 [Index] Committing {len(self._entries)} files...")

        try:
            # 1. 服务端造树 (Server-Side Tree Building)
            tree_hash = self._client._build_tree(self._entries)
            logger.debug(f"   -> Tree built: {tree_hash}")

            # 2. 提交快照
            # 自动处理 Parent Hash (通常是当前的 HEAD)
            commit_hash = self._client.commit(
                tree_hash=tree_hash, message=message, branch=branch, author=author
            )

            logger.info(f"✅ [Index] Commit successful: {commit_hash}")

            # 清空暂存区，防止重复提交
            self._entries.clear()

            return commit_hash

        except Exception as e:
            logger.error(f"❌ [Index] Commit failed: {e}")
            raise e
