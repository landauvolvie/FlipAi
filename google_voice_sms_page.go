package main

// Compatibility name used by the Google Voice SMS WebView. The active detector
// is deliberately background-only: it never clicks, selects, or opens a Google
// Voice conversation when an SMS arrives.
const googleVoiceSMSInitScript = googleVoiceSMSBackgroundInitScript
