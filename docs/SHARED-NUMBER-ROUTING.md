# Shared agent phone-number routing

A single real phone number may be authorized on both Codex and Claude.

For SMS, the configured agent shortcut selects the destination when both agent entries allow texting. With the defaults, `C:` selects Codex and `A:` selects Claude. An unprefixed message uses the configured default agent. If an agent requires its own security code, that code remains first, for example `mycode A: check this`.

Access remains per agent. A number that is Texts only on Codex and Calls only on Claude cannot use `A:` to gain Claude SMS access.

Calls do not carry an SMS shortcut. If both agent entries allow the same caller to call, the default agent receives the call; if only one entry allows calling, that agent receives it.
