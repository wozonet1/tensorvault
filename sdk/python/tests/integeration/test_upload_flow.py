import os
import sys
import time
import uuid
from pathlib import Path
from tensorvault.api.client import Client

# 确保能导入 src 下的代码
# 获取 sdk/python 目录的绝对路径
current_dir = Path(__file__).resolve().parent
sdk_root = current_dir.parents[1]
sys.path.insert(0, str(sdk_root / "src"))


def create_dummy_file(filename, size_mb=10):
    """创建指定大小的随机文件"""
    print(f"🔨 Creating {size_mb}MB dummy file: {filename}...")
    with open(filename, "wb") as f:
        # 写入随机数据确保 Hash 唯一
        f.write(os.urandom(size_mb * 1024 * 1024))


def run_test():
    # 1. 准备环境
    filename = f"test_data_{uuid.uuid4().hex[:8]}.bin"
    create_dummy_file(filename, size_mb=5)  # 5MB 足够测试流式传输

    client = Client("localhost:8080")

    try:
        # --- Scenario 1: Cold Upload (第一次) ---
        print("\n[Scenario 1] Cold Upload (Expect Streaming)...")
        start_time = time.time()

        hash1 = client.upload(filename)

        duration1 = time.time() - start_time
        print("✅ Upload Complete!")
        print(f"   Hash: {hash1}")
        print(f"   Time: {duration1:.4f}s")

        # --- Scenario 2: Warm Upload (第二次) ---
        print("\n[Scenario 2] Warm Upload (Expect Instant/Dedup)...")
        start_time = time.time()

        hash2 = client.upload(filename)

        duration2 = time.time() - start_time
        print("✅ Upload Complete!")
        print(f"   Hash: {hash2}")
        print(f"   Time: {duration2:.4f}s")

        # --- Verification ---
        print("\n[Verification]")
        if hash1 == hash2:
            print("✅ Hash Consistency: PASS")
        else:
            print(f"❌ Hash Consistency: FAIL ({hash1} != {hash2})")

        # 理论上第二次应该极快 (仅网络RTT + DB查询)
        # 第一次涉及 IO 和 S3 上传
        if duration2 < duration1:
            print(
                f"✅ Performance: PASS (Warm {duration2:.4f}s < Cold {duration1:.4f}s)"
            )
        else:
            print("⚠️ Performance: WARN (Warm upload wasn't faster? Check logs.)")

    except Exception as e:
        print("\n❌ Test Failed with Exception:")
        print(e)
    finally:
        # 清理
        client.close()
        if os.path.exists(filename):
            os.remove(filename)
            print(f"\n🧹 Cleaned up {filename}")


if __name__ == "__main__":
    run_test()
