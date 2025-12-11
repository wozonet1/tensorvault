from typing import Optional

import grpc

from tensorvault.v1 import tensorvault_pb2_grpc


class StubManager:
    """
    负责管理 gRPC Channel 和 Stubs 的生命周期。
    实现了 Lazy Loading (懒加载) 和连接复用。
    """

    def __init__(self, target: str):
        self._target = target
        self._channel: Optional[grpc.Channel] = None
        self._meta_stub: Optional[tensorvault_pb2_grpc.MetaServiceStub] = None
        self._data_stub: Optional[tensorvault_pb2_grpc.DataServiceStub] = None

    @property
    def channel(self) -> grpc.Channel:
        """获取或创建 gRPC Channel (单例)"""
        if self._channel is None:
            # 这里的 options 可以配置最大消息大小等
            options = [
                (
                    "grpc.max_send_message_length",
                    1024 * 1024 * 1024,
                ),  # 1GB (原来是100MB)
                ("grpc.max_receive_message_length", 1024 * 1024 * 1024),  # 1GB
                ("grpc.max_metadata_size", 32 * 1024 * 1024),  # 32MB (防止元数据过大)
                # [新增] 优化流控窗口，防止大文件传输卡顿
                ("grpc.http2.max_pings_without_data", 0),
                ("grpc.keepalive_permit_without_calls", 1),
            ]
            # TODO:目前 MVP 阶段使用 insecure channel
            self._channel = grpc.insecure_channel(self._target, options=options)
        return self._channel

    @property
    def meta(self) -> tensorvault_pb2_grpc.MetaServiceStub:
        """获取 MetaService Stub"""
        if self._meta_stub is None:
            self._meta_stub = tensorvault_pb2_grpc.MetaServiceStub(self.channel)
        return self._meta_stub

    @property
    def data(self) -> tensorvault_pb2_grpc.DataServiceStub:
        """获取 DataService Stub"""
        if self._data_stub is None:
            self._data_stub = tensorvault_pb2_grpc.DataServiceStub(self.channel)
        return self._data_stub

    def close(self):
        """关闭连接"""
        if self._channel:
            self._channel.close()
            self._channel = None
