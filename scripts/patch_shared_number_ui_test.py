from pathlib import Path

p = Path('ui_test.go')
text = p.read_text(encoding='utf-8')
old = '''\t// A number reaches one agent. Claiming it for the other has to be refused,
\t// or the allowlist would not answer "who may command this agent".
\trr = a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
\t\t"agent": {"A"}, "newNumber": {"2125550147"}, "newAccess": {"all"},
\t})
\tif rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "one agent only") {
\t\tt.Fatalf("claiming a number for a second agent returned %d: %s", rr.Code, rr.Body.String())
\t}

\tif rr := a.do(t, http.MethodPost, "/agents/numbers/remove", url.Values{"number": {"C:2125550147"}}); rr.Code != http.StatusSeeOther {
\t\tt.Fatalf("remove returned %d: %s", rr.Code, rr.Body.String())
\t}
\tif _, _, ok := agentForSender(a.reloadConfig(t), "2125550147"); ok {
\t\tt.Fatal("the number was not removed")
\t}
'''
new = '''\t// The same real number may be explicitly authorized on Claude too. Because
\t// both copies allow SMS, the incoming sender becomes shared and C:/A: selects
\t// the destination rather than one list silently overriding the other.
\trr = a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
\t\t"agent": {"A"}, "newNumber": {"2125550147"}, "newAccess": {"all"},
\t})
\tif rr.Code != http.StatusSeeOther {
\t\tt.Fatalf("adding the same number to Claude returned %d: %s", rr.Code, rr.Body.String())
\t}
\tcfg = a.reloadConfig(t)
\tagent, phone, ok = agentForSender(cfg, "2125550147")
\tif !ok || agent != "B" || !phone.AllowsSMS() {
\t\tt.Fatalf("shared number did not become an SMS-selectable sender: agent=%q phone=%+v ok=%v", agent, phone, ok)
\t}
\trc, err := parseRemoteCommandForMessage("A: hello Claude", cfg, agent, GmailMessage{})
\tif err != nil || rc.Agent != "A" || rc.Text != "hello Claude" {
\t\tt.Fatalf("shared number did not route A: to Claude: rc=%+v err=%v", rc, err)
\t}

\t// Removing the Codex copy must leave the independently granted Claude copy.
\tif rr := a.do(t, http.MethodPost, "/agents/numbers/remove", url.Values{"number": {"C:2125550147"}}); rr.Code != http.StatusSeeOther {
\t\tt.Fatalf("remove Codex copy returned %d: %s", rr.Code, rr.Body.String())
\t}
\tif agent, _, ok := agentForSender(a.reloadConfig(t), "2125550147"); !ok || agent != "A" {
\t\tt.Fatalf("removing Codex copy also removed Claude permission: agent=%q ok=%v", agent, ok)
\t}
\tif rr := a.do(t, http.MethodPost, "/agents/numbers/remove", url.Values{"number": {"A:2125550147"}}); rr.Code != http.StatusSeeOther {
\t\tt.Fatalf("remove Claude copy returned %d: %s", rr.Code, rr.Body.String())
\t}
\tif _, _, ok := agentForSender(a.reloadConfig(t), "2125550147"); ok {
\t\tt.Fatal("the number remained after both agent copies were removed")
\t}
'''
if text.count(old) != 1:
    raise SystemExit(f'expected one lifecycle block, found {text.count(old)}')
p.write_text(text.replace(old, new, 1), encoding='utf-8')
Path('.github/workflows/patch-shared-number-test.yml').unlink(missing_ok=True)
Path('scripts/patch_shared_number_ui_test.py').unlink(missing_ok=True)
