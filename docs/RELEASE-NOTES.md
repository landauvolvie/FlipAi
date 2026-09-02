# FlipAi v0.46.15

This hotfix fixes ChatGPT `G:` SMS turns that were falsely reported as failed whenever the ChatGPT answer took longer than about eight seconds.

## ChatGPT answers can finish normally

FlipAi uses the same private in-process WebView2 DevTools channel for Google Voice and its dedicated ChatGPT browser. Google Voice intentionally has an 8-second timeout so a stuck page cannot stall call monitoring. In v0.46.14 the ChatGPT turn driver accidentally inherited that same limit even though its page script can wait up to 90 seconds for ChatGPT to finish.

That produced the misleading `Runtime.evaluate` failure after roughly eight seconds while ChatGPT continued working in the WebView and eventually displayed the correct answer.

v0.46.15 keeps the short Google Voice timeout unchanged and gives only the awaited ChatGPT turn operation a 95-second DevTools allowance. Ordinary ChatGPT health/sign-in probes remain short, so the hotfix does not make a wedged browser block normal monitoring.

## Diagnostics and verification

The shared WebView error text is now generic instead of incorrectly calling the ChatGPT page a Google Voice page. Regression coverage verifies that the long timeout is selected only for the ChatGPT awaited turn while ordinary Runtime.evaluate and Google Voice calls retain the 8-second deadline. The app, VERSION file, and installer metadata are aligned on v0.46.15 before the final build and browser test suite runs.
