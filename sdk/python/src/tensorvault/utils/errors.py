class TensorVaultError(Exception):
    """Base class for all TensorVault exceptions."""

    pass


class ServerError(TensorVaultError):
    """Raised when the server returns an internal error."""

    pass


class NetworkError(TensorVaultError):
    """Raised when connection fails."""

    pass


class IntegrityError(TensorVaultError):
    """Raised when data corruption is detected (DataLoss)."""

    pass
