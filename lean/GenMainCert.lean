import AetherCert.Gen

/-- `cert2lean`：把 .cert 证书翻译成一份 Mathlib 可独立核对的 .lean 文件。
    `cert2lean a.cert > a.lean` / `cert2lean - < a.cert`。 -/
def main (args : List String) : IO UInt32 :=
  AetherCert.genMain args
