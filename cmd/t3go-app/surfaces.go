package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

const (
	surfaceFiles = iota
	surfaceChanges
	surfacePullRequest
	surfaceGraph
	surfaceTerminal
	surfacePreview
	surfaceDevices
	surfaceSideChat
	surfaceBrowser
	surfaceCount
)

var surfaceNames = []string{
	"Files", "Changes", "Pull request", "Git graph", "Terminal",
	"Preview", "Devices", "Side chat", "Browser",
}

func surfaceTitle(index int) string {
	if index >= 0 && index < len(surfaceNames) {
		return surfaceNames[index]
	}
	return "Surfaces"
}

func (a *app) openSurface(index int) {
	if index < 0 || index >= surfaceCount {
		index = surfaceFiles
	}
	a.rightTab.Set(index)
	a.rightErr.Set("")
	a.rightOpen.Set(true)
	a.loadSurface(index)
}

func (a *app) closeSurface() {
	a.rightOpen.Set(false)
}

func (a *app) loadSurface(index int) {
	switch index {
	case surfaceFiles:
		a.loadFiles(a.filesPath.Peek())
	case surfaceChanges:
		a.loadChanges()
		a.loadBranches()
	case surfacePullRequest:
		a.loadPullRequests()
	case surfaceGraph:
		a.loadGraph()
	case surfaceTerminal:
		a.loadTerminals()
	case surfacePreview:
		a.loadPreviewServers()
	case surfaceDevices:
		a.loadDevices()
	case surfaceSideChat:
		if thread := a.sideChat.Peek(); thread.ID != "" {
			a.loadSideChat(thread.ID)
		}
	case surfaceBrowser:
		if strings.TrimSpace(a.browserURL.Peek()) == "" {
			a.browserInput.Set("")
		}
	}
}

func (a *app) surfaceThread() (proto.Thread, bool) {
	id := a.selected.Peek()
	if id == "" {
		a.rightErr.Set("Open a thread first.")
		return proto.Thread{}, false
	}
	c := a.client()
	if c == nil {
		a.rightErr.Set("No environment is connected.")
		return proto.Thread{}, false
	}
	thread := a.threadByID(id)
	if thread.ID == "" {
		a.rightErr.Set("This thread is no longer available.")
		return proto.Thread{}, false
	}
	return thread, true
}

func (a *app) loadFiles(path string) {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		entries, err := c.Files(ctx, thread.ID, path)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.filesPath.Set(path)
			a.filesEntries.Set(entries)
			a.fileSelected.Set("")
			a.fileView.Set(proto.WorkspaceFile{})
		})
	}()
}

func (a *app) openFile(path string) {
	thread, ok := a.surfaceThread()
	if !ok || path == "" {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		file, err := c.File(ctx, thread.ID, path)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.fileSelected.Set(path)
			a.fileView.Set(file)
		})
	}()
}

func (a *app) openFileSurface(path string) {
	a.rightTab.Set(surfaceFiles)
	a.rightOpen.Set(true)
	a.openFile(path)
}

func (a *app) fileParent(path string) string {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	if path == "" {
		return ""
	}
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[:index]
	}
	return ""
}

func (a *app) loadChanges() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		working, err := c.WorkingDiff(ctx, thread.ID)
		if err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.rightBusy.Set(false)
					a.rightErr.Set(friendlyError(err))
				}
			})
			return
		}
		patch, diffErr := c.Diff(ctx, thread.ID, "")
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if diffErr != nil {
				a.rightErr.Set(friendlyError(diffErr))
			}
			a.workingDiff.Set(working)
			a.diffText.Set(patch)
		})
	}()
}

func (a *app) loadGraph() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		commits, err := c.Graph(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.graph.Set(commits)
		})
	}()
}

func (a *app) loadPullRequests() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		requests, detail, err := c.PullRequests(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.pullRequests.Set(requests)
			a.prDetail.Set(detail)
		})
	}()
}

func (a *app) loadPullRequestDetail(number int) {
	thread, ok := a.surfaceThread()
	if !ok || number <= 0 {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		view, err := c.PullRequestDetail(ctx, thread.ID, number)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.prView.Set(view)
		})
	}()
}

func (a *app) mergePullRequest(number int, method string, deleteBranch bool) {
	thread, ok := a.surfaceThread()
	if !ok || number <= 0 {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 90*time.Second)
		defer cancel()
		err := c.MergePullRequest(ctx, thread.ID, number, method, deleteBranch)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.gitNote.Set("Pull request merged.")
			a.loadPullRequests()
			a.loadPullRequestDetail(number)
		})
	}()
}

func (a *app) commentPullRequest(number int, body string) {
	thread, ok := a.surfaceThread()
	body = strings.TrimSpace(body)
	if !ok || number <= 0 || body == "" {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		err := c.CommentPullRequest(ctx, thread.ID, number, body)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.gitNote.Set("Comment added.")
			a.loadPullRequestDetail(number)
		})
	}()
}

func (a *app) reviewPullRequest(number int, verdict, body string) {
	thread, ok := a.surfaceThread()
	if !ok || number <= 0 {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		err := c.ReviewPullRequest(ctx, thread.ID, number, verdict, body)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.gitNote.Set("Review submitted.")
			a.loadPullRequestDetail(number)
		})
	}()
}

func (a *app) pullRequestAction(number int, action string) {
	thread, ok := a.surfaceThread()
	if !ok || number <= 0 || action == "" {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		err := c.PullRequestAction(ctx, thread.ID, number, action)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.loadPullRequests()
			a.loadPullRequestDetail(number)
		})
	}()
}

func (a *app) loadTerminals() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		session, err := c.EnsureTerminal(ctx, thread.ID)
		if err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.rightBusy.Set(false)
					a.rightErr.Set(friendlyError(err))
				}
			})
			return
		}
		sessions, err := c.Terminals(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c || a.selected.Peek() != thread.ID {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
			}
			a.terminals.Set(sessions)
			if a.terminalID.Peek() == "" {
				a.terminalID.Set(session.ID)
			}
		})
	}()
}

func (a *app) newTerminal() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		session, err := c.NewTerminal(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.terminalID.Set(session.ID)
			a.loadTerminals()
		})
	}()
}

func (a *app) closeTerminal(id string) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		_ = c.CloseTerminal(ctx, id)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if a.terminalID.Peek() == id {
				a.terminalID.Set("")
			}
			a.loadTerminals()
		})
	}()
}

func (a *app) loadPreviewServers() {
	if a.client() == nil {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		servers, detail, err := c.PreviewServers(ctx)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.previewList.Set(servers)
			a.previewDetail.Set(detail)
		})
	}()
}

func (a *app) openPreview(address string) {
	if strings.TrimSpace(address) == "" {
		return
	}
	a.browserURL.Set(address)
	a.browserInput.Set(address)
	a.rightTab.Set(surfaceBrowser)
}

func (a *app) loadDevices() {
	if a.client() == nil {
		return
	}
	c := a.client()
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		devices, detail, err := c.Devices(ctx)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.devices.Set(devices)
			a.deviceDetail.Set(detail)
		})
	}()
}

func (a *app) selectDevice(id string) {
	a.deviceID.Set(id)
	a.refreshDeviceScreenshot()
}

func (a *app) refreshDeviceScreenshot() {
	id := a.deviceID.Peek()
	c := a.client()
	if c == nil || id == "" {
		return
	}
	a.rightBusy.Set(true)
	a.rightErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 45*time.Second)
		defer cancel()
		image, err := c.DeviceScreenshot(ctx, id)
		if err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.rightBusy.Set(false)
					a.rightErr.Set(friendlyError(err))
				}
			})
			return
		}
		path := filepath.Join(os.TempDir(), fmt.Sprintf("t3go-device-%d.png", time.Now().UnixNano()))
		if err := os.WriteFile(path, image, 0o600); err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.rightBusy.Set(false)
					a.rightErr.Set(friendlyError(err))
				}
			})
			return
		}
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.rightBusy.Set(false)
			a.deviceShot.Set(path)
		})
	}()
}

func (a *app) setDeviceRunning(id string, boot bool) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	a.rightBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		var err error
		if boot {
			err = c.BootDevice(ctx, id)
		} else {
			err = c.ShutdownDevice(ctx, id)
		}
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.rightBusy.Set(false)
			if err != nil {
				a.rightErr.Set(friendlyError(err))
				return
			}
			a.loadDevices()
			if boot {
				a.selectDevice(id)
			}
		})
	}()
}

func (a *app) openBrowser() {
	target := strings.TrimSpace(a.browserInput.Peek())
	if target == "" {
		return
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		a.rightErr.Set("Enter an http or https address.")
		return
	}
	a.browserURL.Set(target)
}

func (a *app) stageAllChanges() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		err := c.StageFiles(ctx, thread.ID, nil)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.gitNote.Set("All changes staged.")
			a.loadChanges()
		})
	}()
}

func (a *app) confirmDiscardChanges() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	ui.Alert(ui.AlertProps{
		Title:   "Discard all changes?",
		Message: "Tracked files return to HEAD and untracked files are removed.",
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() {
				c := a.client()
				a.gitBusy.Set(true)
				go func() {
					ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
					defer cancel()
					err := c.DiscardFiles(ctx, thread.ID, nil)
					ui.Post(func() {
						if a.client() != c {
							return
						}
						a.gitBusy.Set(false)
						if err != nil {
							a.notice.Set(friendlyError(err))
							return
						}
						a.gitNote.Set("Working tree restored.")
						a.loadChanges()
					})
				}()
			}},
		},
	})
}

func (a *app) runGitAction(action proto.GitAction) {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	a.gitNote.Set("")
	message := strings.TrimSpace(a.gitMessage.Peek())
	base := strings.TrimSpace(a.gitBase.Peek())
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 3*time.Minute)
		defer cancel()
		result, err := c.GitAction(ctx, thread.ID, proto.GitActionInput{
			Action:  action,
			Message: message,
			Base:    base,
		})
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			var parts []string
			if result.NoChanges {
				parts = append(parts, "No changes to commit.")
			}
			if result.CommitSHA != "" {
				parts = append(parts, "Committed "+result.CommitSHA[:min(7, len(result.CommitSHA))]+".")
			}
			if result.Pushed {
				parts = append(parts, "Pushed.")
			}
			if result.PRURL != "" {
				parts = append(parts, "Pull request: "+result.PRURL)
			}
			if len(parts) == 0 {
				parts = append(parts, "Done.")
			}
			a.gitNote.Set(strings.Join(parts, " "))
			a.loadChanges()
			a.loadPullRequests()
		})
	}()
}

func (a *app) createBranch(name string) {
	thread, ok := a.surfaceThread()
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		branches, err := c.CreateBranch(ctx, thread.ID, name)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.branches.Set(branches)
			a.gitNote.Set("Created and checked out " + name + ".")
			a.loadChanges()
			a.loadGraph()
		})
	}()
}

func (a *app) switchBranch(name string) {
	thread, ok := a.surfaceThread()
	if !ok || name == "" {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		branches, err := c.SwitchBranch(ctx, thread.ID, name)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.branches.Set(branches)
			a.gitNote.Set("Checked out " + name + ".")
			a.loadChanges()
			a.loadGraph()
		})
	}()
}

func (a *app) pullBranch() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	a.gitBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 90*time.Second)
		defer cancel()
		status, ref, err := c.PullBranch(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.gitBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.gitNote.Set(status + " " + ref)
			a.loadChanges()
		})
	}()
}

func (a *app) loadBranches() {
	thread, ok := a.surfaceThread()
	if !ok {
		return
	}
	c := a.client()
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		branches, err := c.Branches(ctx, thread.ID)
		if err != nil {
			return
		}
		ui.Post(func() {
			if a.client() == c {
				a.branches.Set(branches)
			}
		})
	}()
}

func terminalPage(wsURL string) string {
	encoded, _ := json.Marshal(wsURL)
	return strings.ReplaceAll(`<!doctype html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<style>
  :root{color-scheme:light dark;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
  html,body{margin:0;height:100%;background:#111;color:#ddd}
  body{display:flex;flex-direction:column}
  #out{flex:1;overflow:auto;padding:12px;white-space:pre-wrap;word-break:break-all;font-size:12px;line-height:1.35}
  #bar{display:flex;gap:6px;padding:6px;background:#1b1b1b;border-top:1px solid #333}
  #input{flex:1;min-width:0;background:#151515;color:#eee;border:1px solid #444;border-radius:7px;padding:9px;font:inherit}
  button{background:#292929;color:#eee;border:1px solid #444;border-radius:7px;padding:7px 10px;font:inherit}
  #state{font-size:10px;color:#888;padding:2px 10px}
</style></head><body>
<div id="state">connecting…</div>
<div id="out"></div>
<div id="bar">
  <button id="ctrlc" title="Interrupt">^C</button>
  <input id="input" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="type a command, return to send">
  <button id="send">Send</button>
</div>
<script>
const out=document.getElementById('out'), input=document.getElementById('input'), state=document.getElementById('state');
let ws, sent='';
function text(v){out.textContent+=v; out.scrollTop=out.scrollHeight;}
function send(data){if(ws&&ws.readyState===1) ws.send(JSON.stringify({type:'input',data}));}
function resize(){if(ws&&ws.readyState===1) ws.send(JSON.stringify({type:'resize',cols:Math.max(20,Math.floor(window.innerWidth/8)),rows:Math.max(10,Math.floor((window.innerHeight-90)/18))}));}
function submit(){const value=input.value; if(!value)return; send(value+'\r'); input.value='';}
input.addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();submit();}});
document.getElementById('send').onclick=submit;
document.getElementById('ctrlc').onclick=()=>send('\x03');
function connect(){
  ws=new WebSocket(__WS_URL__);
  ws.onopen=()=>{state.textContent='connected';resize();};
  ws.onmessage=e=>{try{const m=JSON.parse(e.data);if(m.type==='output'&&m.data)text(m.data);if(m.type==='exit'){state.textContent='process exited '+m.code;text('\n[process exited '+m.code+']\n');}}catch(_){}};
  ws.onclose=()=>{state.textContent='disconnected';setTimeout(()=>{state.textContent='reconnecting…';connect()},1200);};
}
connect();
window.addEventListener('resize',resize);
window.addEventListener('beforeunload',()=>{if(ws)ws.close();});
</script></body></html>`, "__WS_URL__", string(encoded))
}

func (a *app) terminalPageHTML() string {
	c := a.client()
	id := a.terminalID.Peek()
	if c == nil || id == "" {
		return ""
	}
	return terminalPage(c.TerminalURL(id))
}

func (a *app) currentRawFileURL(path string) string {
	c := a.client()
	threadID := a.selected.Peek()
	if c == nil || threadID == "" || path == "" {
		return ""
	}
	return c.RawFileURL(threadID, path)
}

func (a *app) threadForSearch(hit proto.MessageHit) bool {
	if hit.ThreadID == "" {
		return false
	}
	for _, thread := range a.threads.Peek() {
		if thread.ID == hit.ThreadID {
			return true
		}
	}
	a.notice.Set("That thread is no longer in this environment.")
	return false
}

func (a *app) applyMessageEventToSide(m proto.Message) {
	a.applySideMessage(m)
}

func (a *app) applyDeltaEventToSide(d proto.MessageDelta) {
	a.applySideDelta(d)
}

func (a *app) surfaceSelectedIndex() int {
	return a.rightTab.Peek()
}

func (a *app) surfaceLabels() []string {
	return surfaceNames
}

func surfaceAccessor(a *app) signal.Accessor[[]string] {
	return signal.Static(surfaceNames)
}
