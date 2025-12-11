import json
import os
import sys
import uuid

from tensorvault.api.client import Client


def create_dummy_files(prefix):
    """创建模拟的实验文件"""
    files = []

    # 1. 模拟一个大文件 (Model Weights)
    bin_name = f"{prefix}_model.bin"
    with open(bin_name, "wb") as f:
        f.write(os.urandom(1024 * 1024))  # 1MB random data
    files.append(bin_name)

    # 2. 模拟一个文本文件 (Config)
    json_name = f"{prefix}_config.json"
    with open(json_name, "w") as f:
        json.dump({"learning_rate": 0.001, "batch_size": 32}, f)
    files.append(json_name)

    return files


def run_test():
    run_id = uuid.uuid4().hex[:8]
    branch_name = f"test-branch-{run_id}"
    print(f"🧪 Starting Workflow Integration Test (Run ID: {run_id})")

    # 1. 准备数据
    files = create_dummy_files(run_id)
    print(f"   Created {len(files)} dummy files.")

    client = Client("localhost:8080")

    try:
        # --- Step 1: Staging (Index Add) ---
        print("\n[Step 1] Staging files (Upload & Index)...")

        # 获取一个新的 Index 对象
        idx = client.new_index()

        for local_path in files:
            # 模拟存放到服务端的特定目录下
            remote_path = f"experiments/{run_id}/{local_path}"
            hash_val = idx.add(local_path, remote_path=remote_path)
            print(
                f"   + Added: {local_path} -> {remote_path} (Hash: {hash_val[:8]}...)"
            )

        # --- Step 2: Commit ---
        print("\n[Step 2] Committing snapshot...")
        commit_msg = f"Benchmark Run {run_id}"

        # 这一步会触发: BuildTree RPC -> Commit RPC
        commit_hash = idx.commit(
            message=commit_msg, branch=branch_name, author="IntegrationBot"
        )

        if not commit_hash:
            print("❌ Commit Failed: Returned empty hash")
            sys.exit(1)

        print(f"✅ Commit Success! Hash: {commit_hash}")

        # --- Step 3: Verification (Get Ref) ---
        print("\n[Step 3] Verifying Reference on Server...")

        # 调用 GetRef RPC 确认服务器真的更新了分支指针
        server_hash = client.get_ref(branch_name)

        print(f"   Local Commit Hash:  {commit_hash}")
        print(f"   Server Branch Hash: {server_hash}")

        if commit_hash == server_hash:
            print("✅ Verification PASS: Branch updated correctly.")
        else:
            print("❌ Verification FAIL: Hash mismatch!")
            sys.exit(1)

    except Exception:
        print("\n❌ Test Failed with Exception:")
        import traceback

        traceback.print_exc()
        sys.exit(1)

    finally:
        client.close()
        # 清理本地垃圾文件
        for f in files:
            if os.path.exists(f):
                os.remove(f)
        print("\n🧹 Cleaned up local files.")


if __name__ == "__main__":
    run_test()
