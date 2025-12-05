from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CheckFileRequest(_message.Message):
    __slots__ = ()
    SHA256_FIELD_NUMBER: _ClassVar[int]
    SIZE_FIELD_NUMBER: _ClassVar[int]
    sha256: str
    size: int
    def __init__(
        self, sha256: _Optional[str] = ..., size: _Optional[int] = ...
    ) -> None: ...

class CheckFileResponse(_message.Message):
    __slots__ = ()
    EXISTS_FIELD_NUMBER: _ClassVar[int]
    MERKLE_ROOT_HASH_FIELD_NUMBER: _ClassVar[int]
    exists: bool
    merkle_root_hash: str
    def __init__(
        self, exists: _Optional[bool] = ..., merkle_root_hash: _Optional[str] = ...
    ) -> None: ...

class UploadRequest(_message.Message):
    __slots__ = ()
    META_FIELD_NUMBER: _ClassVar[int]
    CHUNK_DATA_FIELD_NUMBER: _ClassVar[int]
    meta: FileMeta
    chunk_data: bytes
    def __init__(
        self,
        meta: _Optional[_Union[FileMeta, _Mapping]] = ...,
        chunk_data: _Optional[bytes] = ...,
    ) -> None: ...

class FileMeta(_message.Message):
    __slots__ = ()
    PATH_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    path: str
    sha256: str
    def __init__(
        self, path: _Optional[str] = ..., sha256: _Optional[str] = ...
    ) -> None: ...

class UploadResponse(_message.Message):
    __slots__ = ()
    HASH_FIELD_NUMBER: _ClassVar[int]
    TOTAL_SIZE_FIELD_NUMBER: _ClassVar[int]
    hash: str
    total_size: int
    def __init__(
        self, hash: _Optional[str] = ..., total_size: _Optional[int] = ...
    ) -> None: ...

class DownloadRequest(_message.Message):
    __slots__ = ()
    HASH_FIELD_NUMBER: _ClassVar[int]
    hash: str
    def __init__(self, hash: _Optional[str] = ...) -> None: ...

class DownloadResponse(_message.Message):
    __slots__ = ()
    CHUNK_DATA_FIELD_NUMBER: _ClassVar[int]
    chunk_data: bytes
    def __init__(self, chunk_data: _Optional[bytes] = ...) -> None: ...

class GetHeadRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetHeadResponse(_message.Message):
    __slots__ = ()
    HASH_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    EXISTS_FIELD_NUMBER: _ClassVar[int]
    hash: str
    version: int
    exists: bool
    def __init__(
        self,
        hash: _Optional[str] = ...,
        version: _Optional[int] = ...,
        exists: _Optional[bool] = ...,
    ) -> None: ...

class CommitRequest(_message.Message):
    __slots__ = ()
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    AUTHOR_FIELD_NUMBER: _ClassVar[int]
    TREE_HASH_FIELD_NUMBER: _ClassVar[int]
    PARENT_HASHES_FIELD_NUMBER: _ClassVar[int]
    BRANCH_NAME_FIELD_NUMBER: _ClassVar[int]
    message: str
    author: str
    tree_hash: str
    parent_hashes: _containers.RepeatedScalarFieldContainer[str]
    branch_name: str
    def __init__(
        self,
        message: _Optional[str] = ...,
        author: _Optional[str] = ...,
        tree_hash: _Optional[str] = ...,
        parent_hashes: _Optional[_Iterable[str]] = ...,
        branch_name: _Optional[str] = ...,
    ) -> None: ...

class CommitResponse(_message.Message):
    __slots__ = ()
    COMMIT_HASH_FIELD_NUMBER: _ClassVar[int]
    commit_hash: str
    def __init__(self, commit_hash: _Optional[str] = ...) -> None: ...
