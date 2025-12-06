import os
import sys
import time
import uuid
from pathlib import Path

import numpy as np
import pandas as pd

from tensorvault.api.client import Client

# 确保能导入 src 下的代码
# 路径: sdk/python/tests/integration/ -> sdk/python/src
current_dir = Path(__file__).resolve().parent
sdk_root = current_dir.parents[1]
sys.path.insert(0, str(sdk_root / "src"))


def create_dummy_csv(filename, rows=1000):
    """创建一个包含随机数据的 Pandas DataFrame 并保存为 CSV"""
    print(f"🔨 Creating dummy CSV: {filename} ({rows} rows)...")
    df = pd.DataFrame(np.random.randint(0, 100, size=(rows, 4)), columns=list("ABCD"))
    # 添加一列字符串，增加复杂度
    df["E"] = [f"uuid-{uuid.uuid4().hex[:4]}" for _ in range(rows)]

    df.to_csv(filename, index=False)
    return df


def run_test():
    filename = f"test_dataset_{uuid.uuid4().hex[:8]}.csv"
    original_df = create_dummy_csv(filename)

    client = Client("localhost:8080")

    try:
        # --- Step 1: Upload (Push) ---
        print("\n[Step 1] Uploading CSV to TensorVault...")
        start_time = time.time()
        merkle_root = client.upload(filename)
        print(f"✅ Upload Success! Merkle Root: {merkle_root}")
        print(f"   Time taken: {time.time() - start_time:.4f}s")

        # --- Step 2: Download & Read via Pandas (Pull) ---
        print("\n[Step 2] Reading directly into Pandas via client.open()...")
        start_time = time.time()

        # 核心验证点：client.open 返回的是一个 file-like object
        # Pandas 应该能直接从这个流中读取数据，无需下载到本地文件
        with client.open(merkle_root) as f:
            downloaded_df = pd.read_csv(f)

        print("✅ Read Success!")
        print(f"   Time taken: {time.time() - start_time:.4f}s")
        print(f"   DataFrame Shape: {downloaded_df.shape}")

        # --- Step 3: Verification ---
        print("\n[Step 3] Verifying Data Integrity...")

        # 验证内容是否完全一致
        pd.testing.assert_frame_equal(original_df, downloaded_df)
        print("✅ Dataframes match perfectly!")

    except Exception:
        print("\n❌ Test Failed with Exception:")
        import traceback

        traceback.print_exc()
        sys.exit(1)

    finally:
        client.close()
        if os.path.exists(filename):
            os.remove(filename)
            print(f"\n🧹 Cleaned up {filename}")


if __name__ == "__main__":
    run_test()
