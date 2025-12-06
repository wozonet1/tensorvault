import os

try:
    import google.protobuf.descriptor_pb2
    import google.protobuf.duration_pb2
    import google.protobuf.timestamp_pb2
    import google.protobuf.wrappers_pb2  # noqa: F401

    # 如果还有其他报错缺少的，继续加在这里
except ImportError:
    # 如果连这些都导不入，说明环境里的 protobuf 库坏了
    pass
from tensorvault.api.client import Client

os.environ["PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION"] = "python"

__all__ = ["Client"]
