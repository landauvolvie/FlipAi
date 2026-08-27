//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// sendGoogleVoiceImageMMS deliberately bypasses the Gmail reply gateway.
// Google Voice accepts text replies through @txt.voice.google.com, but that
// gateway drops image attachments. FlipAi therefore reuses the already-running
// signed-in Edge Google Voice surface and drives the normal image-send UI over
// the loopback-only DevTools channel that the call receiver already owns.
func sendGoogleVoiceImageMMS(ctx context.Context, original GmailMessage, body string, img *outboundVoiceImage) error {
	if img == nil || len(img.Data) == 0 {
		return errors.New("generated image is empty")
	}
	if len(img.Data) > voiceImageMaxBytes {
		return errors.New("generated image is too large for Google Voice MMS")
	}
	phone := googleVoiceImageRecipient(original)
	if phone == "" {
		return errors.New("could not determine the Google Voice SMS recipient")
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return err
	}
	if err := ensureDataDir(dataDir); err != nil {
		return err
	}

	tempDir := filepath.Join(dataDir, "outbound-mms")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return fmt.Errorf("create outbound MMS folder: %w", err)
	}
	token, err := secureRandomToken(12)
	if err != nil {
		return err
	}
	ext := ".png"
	switch strings.ToLower(strings.TrimSpace(img.MediaType)) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/png":
	default:
		return fmt.Errorf("unsupported Google Voice image type %q", img.MediaType)
	}
	imagePath := filepath.Join(tempDir, "flipai-"+token+ext)
	if err := os.WriteFile(imagePath, img.Data, 0600); err != nil {
		return fmt.Errorf("stage Google Voice image: %w", err)
	}
	defer os.Remove(imagePath)
	imagePath, _ = filepath.Abs(imagePath)

	return requestGoogleVoiceMMS(ctx, dataDir, phone, strings.TrimSpace(body), imagePath)
}

func googleVoiceImageRecipient(m GmailMessage) string {
	if n, ok := senderFromVoiceAddress(m.ReplyTo); ok {
		return n
	}
	if n, ok := senderFromVoiceAddress(m.From); ok {
		return n
	}
	for _, match := range subjectPhoneRE.FindAllString(m.Subject, -1) {
		if n := normalizeUSPhone(match); n != "" {
			return n
		}
	}
	return ""
}

// The image is sent by the process that owns the Google Voice view, not by the
// one that decided to send it.
//
// WebView2 is reached in-process -- there is no port to connect to from
// outside -- so the host asks the Google Voice process to do it, over a
// loopback endpoint that process opens for exactly this. The port is written
// into the runtime state file and the request carries FlipAi's own local token,
// so nothing else on the machine can drive the signed-in Google Voice session.
func requestGoogleVoiceMMS(ctx context.Context, dataDir, phone, caption, imagePath string) error {
	if err := platformOpenGoogleVoice(dataDir, false); err != nil {
		return fmt.Errorf("open Google Voice for MMS: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"phone":   phone,
		"caption": caption,
		"image":   imagePath,
	})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		port, token := platformVoiceControlEndpoint(dataDir)
		if port <= 0 || token == "" {
			last = errNoVoiceControlChannel
		} else {
			err := postVoiceControl(ctx, port, token, "/mms", body)
			if err == nil {
				return nil
			}
			last = err
			// A refusal from the page itself will not become a success by
			// being repeated; only a channel that is not up yet will.
			if !errors.Is(err, errVoiceControlUnreachable) {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if last == nil {
		last = errNoVoiceControlChannel
	}
	return fmt.Errorf("Google Voice MMS is unavailable: %w", last)
}

// errVoiceControlUnreachable separates "the Google Voice process is not
// listening yet" from "it listened and said no".
var errVoiceControlUnreachable = errors.New("the Google Voice process is not listening yet")

func postVoiceControl(ctx context.Context, port int, token, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:"+strconv.Itoa(port)+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FlipAi-Token", token)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errVoiceControlUnreachable, err)
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("%w: %s", errVoiceControlUnreachable, strings.TrimSpace(string(text)))
	}
	return fmt.Errorf("%s", strings.TrimSpace(string(text)))
}

// sendGoogleVoiceMMSInPage is the half that runs inside the Google Voice
// process: prepare the conversation, attach the image to the picker's file
// input, and send.
func sendGoogleVoiceMMSInPage(d voiceDevTools, phone, caption, imagePath string) error {
	snap, err := voiceReadSnapshot(d)
	if err != nil {
		return err
	}
	if !snap.SignedIn {
		return errors.New("Google Voice is not signed in inside FlipAi")
	}

	phoneJSON, _ := json.Marshal(phone)
	captionJSON, _ := json.Marshal(caption)
	prepare := strings.ReplaceAll(voicePrepareMMSJS, "__PHONE__", string(phoneJSON))
	prepare = strings.ReplaceAll(prepare, "__CAPTION__", string(captionJSON))
	var stage string
	if err := voiceEval(d, prepare, true, &stage); err != nil {
		return fmt.Errorf("prepare Google Voice MMS: %w", err)
	}
	if stage != "ready" {
		return fmt.Errorf("prepare Google Voice MMS: %s", stage)
	}

	objectID, err := voiceEvalObject(d, voiceImageInputObjectJS)
	if err != nil {
		return fmt.Errorf("Google Voice image picker did not expose a file input: %w", err)
	}
	// Some builds will not accept an element handle until the DOM domain is
	// on. Failing that check looks exactly like an image that was never sent.
	_ = d.Call("DOM.enable", map[string]any{}, nil)
	if err := d.Call("DOM.setFileInputFiles", map[string]any{
		"files":    []string{imagePath},
		"objectId": objectID,
	}, nil); err != nil {
		return fmt.Errorf("attach image in Google Voice: %w", err)
	}

	if err := voiceEval(d, voiceAfterFileSelectJS, true, &stage); err != nil {
		return fmt.Errorf("send Google Voice MMS: %w", err)
	}
	if stage != "sent" {
		return fmt.Errorf("send Google Voice MMS: %s", stage)
	}
	return nil
}

const voicePrepareMMSJS = `(async () => {
  const phone = __PHONE__;
  const caption = __CAPTION__;
  const sleep = (ms) => new Promise(r => setTimeout(r, ms));
  const docs = () => {
    const out = [document];
    for (let i = 0; i < out.length; i++) {
      let frames = [];
      try { frames = out[i].querySelectorAll('iframe,frame'); } catch (_) {}
      for (const f of frames) {
        try { const d = f.contentDocument; if (d && !out.includes(d)) out.push(d); } catch (_) {}
      }
    }
    return out;
  };
  const all = (sel) => {
    const out = [];
    for (const d of docs()) { try { out.push(...d.querySelectorAll(sel)); } catch (_) {} }
    return out;
  };
  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const name = (el) => ((el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('placeholder'))) || '') + ' ' + (el.innerText || el.textContent || '');
  const normName = (el) => name(el).replace(/\s+/g, ' ').trim();
  const buttons = () => all('button,[role="button"]');
  const clickNamed = (re) => {
    const b = buttons().find(x => visible(x) && !x.disabled && re.test(normName(x)));
    if (!b) return false;
    b.click();
    return true;
  };
  const setValue = (el, value) => {
    try {
      const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
      setter.call(el, value);
    } catch (_) { el.value = value; }
    el.dispatchEvent(new Event('input', {bubbles:true}));
    el.dispatchEvent(new Event('change', {bubbles:true}));
  };
  const digits = (v) => String(v || '').replace(/\D/g, '').replace(/^1(?=\d{10}$)/, '');

  if (location.hostname.toLowerCase() !== 'voice.google.com') return 'not-on-google-voice';
  const body = String((document.body && document.body.innerText) || '');
  if (/^\s*sign\s+in\s*$/im.test(body.slice(0, 1600))) return 'not-signed-in';

  // Open the Messages surface first when Voice is sitting on Calls/Voicemail.
  clickNamed(/^(messages|text messages)$/i);
  await sleep(350);

  // Start a fresh compose. Voice folds an exact phone number back into its
  // existing conversation, so we do not need a private conversation id.
  if (!clickNamed(/^(send a message|send new message|new message|compose|start a message)$/i)) {
    // If a conversation is already open, the compose button can be icon-only;
    // a looser aria/title match catches that without confusing the final Send.
    clickNamed(/(send new message|new message|compose|start message)/i);
  }
  await sleep(450);

  let recipient = all('input').find(el => visible(el) && /(name|phone|recipient|^to\b)/i.test(normName(el)));
  if (!recipient) return 'recipient-input-missing';
  recipient.focus();
  setValue(recipient, phone);
  await sleep(700);

  let picked = false;
  const choices = all('[role="option"],[role="listbox"] [role="option"],[role="menuitem"],mat-option,gv-contact-list-item').filter(visible);
  const exact = choices.find(choice => digits(normName(choice)).includes(phone));
  if (exact) {
    exact.click();
    picked = true;
  } else if (choices.length === 1) {
    // The only suggestion after typing an exact 10-digit number is safe to
    // select even when Voice renders the number in an element whose text is
    // hidden from our accessibility-name helper.
    choices[0].click();
    picked = true;
  }
  if (!picked) {
    recipient.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true}));
    recipient.dispatchEvent(new KeyboardEvent('keyup', {key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true}));
  }
  await sleep(550);

  const composer = all('textarea,input,[contenteditable="true"]')
    .find(el => visible(el) && el !== recipient && /(message|text|sms|type)/i.test(normName(el)));
  if (caption && composer) {
    composer.focus();
    if (composer.isContentEditable) {
      composer.textContent = caption;
      composer.dispatchEvent(new InputEvent('input', {bubbles:true, inputType:'insertText', data:caption}));
    } else {
      setValue(composer, caption);
    }
  }

  // Google's help text calls this control "Select image". Older/current Voice
  // builds have also exposed Send image, Attach image, or icon-only variants.
  let attachmentClicked = clickNamed(/^(select image|send image|attach image|add image|image|photo)$/i);
  if (!attachmentClicked) attachmentClicked = clickNamed(/(select.*(image|photo)|attach.*(image|photo)|(image|photo).*attach|send.*image)/i);
  if (attachmentClicked) await sleep(400);

  const fileInput = all('input[type="file"]').find(el => {
    const accept = String(el.getAttribute('accept') || '').toLowerCase();
    return !accept || accept.includes('image') || accept.includes('png') || accept.includes('jpeg') || accept.includes('jpg') || accept.includes('gif');
  });
  return fileInput ? 'ready' : 'file-input-missing';
})()`

const voiceImageInputObjectJS = `(() => {
  const docs = [document];
  for (let i = 0; i < docs.length; i++) {
    let frames = [];
    try { frames = docs[i].querySelectorAll('iframe,frame'); } catch (_) {}
    for (const f of frames) {
      try { const d = f.contentDocument; if (d && !docs.includes(d)) docs.push(d); } catch (_) {}
    }
  }
  for (const d of docs) {
    let inputs = [];
    try { inputs = d.querySelectorAll('input[type="file"]'); } catch (_) {}
    for (const el of inputs) {
      const accept = String(el.getAttribute('accept') || '').toLowerCase();
      if (!accept || accept.includes('image') || accept.includes('png') || accept.includes('jpeg') || accept.includes('jpg') || accept.includes('gif')) return el;
    }
  }
  return null;
})()`

const voiceAfterFileSelectJS = `(async () => {
  const sleep = (ms) => new Promise(r => setTimeout(r, ms));
  const docs = () => {
    const out = [document];
    for (let i = 0; i < out.length; i++) {
      let frames = [];
      try { frames = out[i].querySelectorAll('iframe,frame'); } catch (_) {}
      for (const f of frames) {
        try { const d = f.contentDocument; if (d && !out.includes(d)) out.push(d); } catch (_) {}
      }
    }
    return out;
  };
  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const name = (el) => (((el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title'))) || '') + ' ' + (el.innerText || el.textContent || '')).replace(/\s+/g, ' ').trim();

  // Image decoding/upload preview is asynchronous. Poll for the enabled Send
  // control instead of assuming a fixed sub-second delay.
  for (let attempt = 0; attempt < 24; attempt++) {
    await sleep(attempt === 0 ? 700 : 350);
    const candidates = [];
    for (const d of docs()) {
      try { candidates.push(...d.querySelectorAll('button,[role="button"]')); } catch (_) {}
    }
    const send = candidates.find(el => visible(el) && !el.disabled && /^(send|send message|send text|send sms)$/i.test(name(el)));
    if (send) {
      send.click();
      await sleep(800);
      return 'sent';
    }
  }
  return 'send-button-missing';
})()`
