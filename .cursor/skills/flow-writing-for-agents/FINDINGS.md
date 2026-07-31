# 常驻材料审计结果

用 [SKILL.md](SKILL.md) 的判据审本工作区 `alwaysApply: true` 的规则文件得到的结果。这是一份快照，修掉哪条就划掉哪条。

审计时的基线：5 个常驻文件，共 262 行。

## 完全重复的常驻规则

`idp/.cursor/rules/idp-mcp-messenger.mdc` 和 `idp/idp-bento-go/.cursor/rules/idp-mcp-messenger.mdc` 逐字节相同，各 49 行，两份都常驻。98 行的上下文预算买了 49 行的意思。

违反的是**单一事实来源**：一个意思应该只在一个权威位置存在。

修法：留 `idp/.cursor/rules/` 那份，它同时覆盖所有 `idp-*` 仓库。

## 指向不存在工具的指针

`.cursor/rules/project-workflow.mdc` 是最大的常驻文件（73 行），通篇要求调用 `pwzn_wx` / `cxld_jw` / `jh8t_io`。遍历本项目所有 MCP 服务器的工具清单，这三个工具都不存在，提供它们的 `project-dev-workflow` 服务器也不存在。

违反的是**相关性**：73 行常驻指令描述的世界已经不在了。

同类问题：`my-mcp.mdc`（56 行）和 `mcp-messenger.mdc`（17 行）围绕 `my-mcp-N` 通道写，而实际存在的是 `idp-mcp-6` 到 `idp-mcp-12`。这两份文件引用的工具名（`check_messages`、`dispatch_task`、`report_task`、`list_sessions`、`query_tasks`）真实存在，只是挂在别的通道上——所以是通道名陈旧，协议内容还活着。

## 同一协议的五份写法

`mcp-messenger.mdc`（17 行）、`my-mcp.mdc`（56 行）、`project-workflow.mdc`（73 行），加上两份 `idp-mcp-messenger.mdc`（各 49 行），编码的是同一个协议：每轮结束调用一个阻塞式检查工具，收到消息就处理，处理完再调，永续循环。区别只在工具名和措辞。

违反的是**沉积**：新的写法一层层落下来，旧的没人敢删。

五个文件 244 行承载一个意思，收敛到一个文件约 65 行即可，其中 90 行本来就指向不存在的东西。

## 否定式密度

262 行常驻材料里有 25 处禁令式表述（禁止 / 不得 / 不要 / NEVER / Do not）。

违反的是**正向表述**：禁令会把被禁的行为拖进上下文，反而让它更容易被激活。

两个可以直接改的例子：

- `my-mcp.mdc` 的「**禁止**用『工具正常』『通道已验证』等套话代替回答」→「像平常聊天一样直接回应用户的问题」
- `project-workflow.mdc` 的「Produce **zero** user-visible tokens」→「静默重试」

## 无指针的活能力

`user-pchat` 服务器提供 28 个工具（计划、委派、写锁、backlog、任务生命周期），没有任何规则文件或文档提到它。

违反的是**两种预算**的分配：这份能力完全骑在认知预算上——只有记得它存在的人才用得上。

修法：在保留的那份常驻规则里加一行指针，或者写一份外置参考。
