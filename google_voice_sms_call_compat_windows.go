//go:build windows

package main

// The Google Voice calling process historically initialized the direct-SMS
// page detector. Direct SMS now runs only in its own authenticated background
// worker, so keep the old hook as a no-op to leave the calling code path itself
// unchanged.
const googleVoiceSMSInitScript = `(()=>{})()`
