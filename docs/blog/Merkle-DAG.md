# TensorVault 开发手记 #2：Markel DAG

## 背景

在上一篇 [TensorVault 开发手记 #1](./week1.md) 中，我们确定了使用 FastCDC 算法将 AI 大模型切分为变长的、去重友好的数据块（Chunks）。
现在，我们面临一个新的挑战：如何将成千上万个散落的 Chunk，重新组装成一个完整的文件，甚至是一个包含多层目录的版本快照？

Git 的对象模型非常经典，但它将文件视为一个整体（Blob），这在 TensorVault 面对 GB 级模型时不再适用——我们不能让一个对象大到塞不进内存。我们需要重新设计一套核心对象模型 (Core Object Model)。

## 方案推演：从清单到图

在确定最终方案前，我思考了以下几种可能的路径：

- **扁平清单 (Flat List)**：最直观的做法是维护一个包含所有 Chunk Hash 的列表文件。但有如下问题:

  - **随机读取难**：如果文件有 1TB，清单本身就有几百 MB。想读文件的最后一部分，得把整个清单加载进内存算出偏移量，效率低。

  - **缺乏结构复用**：如果两个文件只有中间一部分不同，扁平清单很难表达“目录树”的层级复用关系。

  - **完整性校验慢**：必须下载完所有 Chunk 才能校验整个文件是否损坏。

- **标准 Merkle Tree**：类似 BitTorrent，通过树状结构解决了完整性校验和随机读取的问题，但在处理复杂的版本历史和跨版本去重时，结构略显僵化。

最终，我将目光投向了 [IPFS (InterPlanetary File System)](https://docs.ipfs.tech/) 背后的核心数据结构——**Merkle DAG (默克尔有向无环图)**。

Merkle DAG 结合了 Merkle Tree 的完整性校验能力和 DAG 的拓扑灵活性。它不仅能将大文件组织成树，还能完美地表示文件目录结构、提交历史（Commit Graph），并天然支持数据去重（不同的文件引用同一个子节点）。
这正是 TensorVault 作为一个分布式 AI 资产管理引擎所需要的骨架。

## 原理与设计

Merkel DAG 可以分为两部分来理解:

- 第一部分是**Merkel(默克尔特性)**,即父节点的 Hash 是根据子节点的 Hash,这确保了一旦子节点的内容(与 Hash)改变,父节点的 Hash 也会变,最终根节点的 Hash 也改变,反过来说,如果根节点的哈希值没有改变,那么整棵树一定都没有改变
- 第二部分是**DAG(Driected Acyclic Graph - 有向无环图)**,即边是有向边,但是没有环的图,这是为了能够让同一个节点被多次索引(在图中被多次指向),因而对于重复节点不必重复储存,实现结构化去重

以上是最基本的模型构想,我们接下来需要考虑这种数据模型如何能够套到我们的场景中,并且需要进行哪些适配性设计与抽象封装设计,在这个过程中,可以参考[A Terse, Quick IPLD Primer for the Engineer](https://ipld.io/docs/intro/primer/)提供的诸多指导原则

首当其冲的自然是我们前面提到的大文件的各个块如何组织成树,这需要我们设计一个**ADL(Advanced Data Layout)**

### 设计 ADL (Advanced Data Layout)：FileNode

我们在上一篇中通过 FastCDC 得到了一堆散落的 Chunk (数据块)。但在用户的视角里，他们操作的是一个完整的 10GB 模型文件。
这就需要引入 IPLD 中的 ADL (高级数据布局) 概念。简单来说，我们需要设计一种**胶水节点**，它的唯一作用就是记录：“这个大文件是由 Chunk A、Chunk B、Chunk C... 按顺序组成的”。
在 TensorVault 中，我定义了这个胶水节点为 FileNode：

```Go
// Link: 封装了 Tag 42 和 Multibase 前缀的哈希指针
type Link struct { Hash string }

// FileNode: AI 大文件的“元数据清单” (ADL)
type FileNode struct {
    Type      ObjectType
    TotalSize int64
    Chunks    []ChunkLink // 使用 Link 类型，而非 string
}
```

#### 架构决策：

Git 将文件视为一个整体 Blob，这在处理小文本时很高效。但在 AI 场景下，FileNode 的引入实现了**物理存储与逻辑视图的解耦**。底层是去重的碎片，上层是完整的文件。这也为未来实现随机读取 (Random Access) 和 并行下载 提供了数据结构基础。 2. 完整的对象模型 (The Core Object Model)
解决了大文件的表示问题后，剩下的就是经典的 Merkle DAG 结构了。为了适应 AI 资产管理的需求，我设计了以下四层对象模型：

1. L1 Chunk (数据块): 最底层的物理二进制数据，由 FastCDC 切分而来。
2. L2 FileNode (文件节点): 上述的 ADL 结构，将碎片拼装为逻辑文件。
3. L3 Tree (目录节点): 映射文件系统的目录结构，记录 Filename -> Hash 的映射。
4. L4 Commit (版本节点): 记录版本历史、作者信息、时间戳以及指向根目录的 TreeHash。

这构成了一个**自底向上**的校验体系：任何一个底层的 Chunk 发生比特翻转，都会导致 FileNode Hash 变化，进而导致 Tree Hash 变化，最终改变 Commit Hash。这就是数据完整性的数学保证。

### 3. 序列化协议：为什么是 DAG-CBOR？

有了结构体，我们还需要一种方式将它们序列化为二进制。我选择了 DAG-CBOR（IPLD 规范的 CBOR 子集）。
相比于普通的 JSON 或 Protobuf，DAG-CBOR 在 TensorVault 中解决了三个核心问题：

- **二进制原生**：原生支持 []byte，无需 Base64 转码，对于存储 Hash 和加密数据极其高效。
- **确定性编码 (Canonical Encoding)**：通过强制 Map Key 排序和最短整数编码等，保证了 Merkle Root 的数学唯一性。
- **链接语义 (Tag 42)**：这是最关键的一点。普通的 Hash 只是字符串，而 DAG-CBOR 引入了 Tag 42。我在底层实现了 Link 类型的自定义序列化逻辑，当数据落盘时，Hash 会自动被标记为“链接”。这不仅让数据具备了语义，更为未来与 IPFS 生态的互通留下了接口。

## 总结与预告

至此，TensorVault 的骨架已经搭建完毕：
FastCDC 负责将大象切成肉丁。
Merkle DAG 负责把肉丁拼回大象，并管理大象的家族族谱。
CBOR 负责把这些信息压缩打包。
理论与设计已经闭环。在下一篇开发手记中，我将分享**存储层的实现**：如何将这些设计好的对象，持久化到 MySQL (元数据) 和 MinIO (对象存储) 中，实现真正的存算分离。
