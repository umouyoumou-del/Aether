/-
  AetherCert.Main —— `certcheck` 可执行

  用法：

      certcheck 证书文件
      certcheck -            （从标准输入读）

  退出码：`0` 通过，`1` 被拒，`2` 用法或读取错误。被拒时把原因打到 stderr，
  格式里带行号，所以可以直接与证书文件对照。

  这里刻意不做的事：不读配置文件、不联网、不做任何「顺手」的便利功能。
  校验器越简单，它的「通过」才越有意义。
-/
import AetherCert.Check

open AetherCert

def usage : String :=
  "用法：certcheck <证书文件>      从文件读\n" ++
  "      certcheck -              从标准输入读"

/-- 把结果打印出去，返回退出码。 -/
def report (r : Result) : IO UInt32 := do
  match r with
  | .ok fin n =>
      IO.println s!"通过：{n} 步全部成立，最终项 {fin}"
      return (0 : UInt32)
  | .err msg =>
      IO.eprintln s!"拒绝：{msg}"
      return (1 : UInt32)

def main (args : List String) : IO UInt32 := do
  match args with
  | [path] =>
      let text : String ←
        if path = "-" then do
          let stdin ← IO.getStdin
          stdin.readToEnd
        else
          try
            IO.FS.readFile path
          catch e =>
            IO.eprintln s!"读不到文件 {path}：{e}"
            return (2 : UInt32)
      match checkText text with
      | .error e =>
          IO.eprintln s!"拒绝：{e}"
          return (1 : UInt32)
      | .ok r => report r
  | _ =>
      IO.eprintln usage
      return (2 : UInt32)