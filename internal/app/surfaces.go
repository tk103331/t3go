package app

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// surfaceNotice is the quiet line a surface shows when nothing is open.
func surfaceNotice(c *ui.Context, text string) {
	ui.Column(c).Grow(1).Center().Padding(20).Children(func() {
		ui.Text(c, text).FontSize(12.5).TextColor(c.Theme().TextMuted).MaxLines(6).TextAlign(ui.Center)
	})
}

// surfaceHeader is one surface's title row with its refresh action.
func surfaceHeader(c *ui.Context, title string, refresh func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(0, 0, 6, 0).Children(func() {
		text(c, title, 12, t.Text, 600).Grow(1)
		if refresh != nil {
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh"), refresh)
		}
	})
}

// pullRequestSurface shows the branch's open pull request, or offers one.
func (s *Session) pullRequestSurface(c *ui.Context) {
	t := c.Theme()
	if s.selected == "" {
		surfaceNotice(c, i18n.T("Pick a thread to read its pull request."))
		return
	}
	// One PR the reader opened takes the pane; otherwise the branch's
	// open PR, or the offer to create one.
	if s.prViewNumber > 0 {
		s.pullRequestDetail(c)
		return
	}
	if len(s.pullRequests) > 0 {
		s.linkedPullRequests(c)
		return
	}
	pr := s.pullRequest
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		if s.gitBusy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Running git action"))
				faint(c, i18n.T("Working…"), 12)
			})
		}
		if pr.URL != "" {
			ui.Text(c, pr.Title).FontSize(13).FontWeight(600).FixedLineHeight(18).TextColor(t.Text).MaxLines(3)
			faint(c, fmt.Sprintf("#%d · %s", pr.Number, strings.ToLower(pr.State)), 11.5)
			ui.Text(c, pr.HeadBranch+" → "+pr.BaseBranch).FontSize(11.5).Font("monospace").
				TextColor(t.TextMuted).SingleLine()
			ui.Text(c, pr.URL).FontSize(11.5).TextColor(t.Accent).MaxLines(2).Selectable()
			ui.Row(c).Gap(6).Padding(0, 0, 4, 0).Children(func() {
				ui.PrimaryButton(c, "").Label(i18n.T("Open pull request")).Children(func() {
					ui.Icon(c, lucide.GitPullRequest()).FontSize(15)
					ui.Text(c, i18n.T("Open pull request"))
				}).OnClick(func() { c.OpenURL(pr.URL) })
				iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh pull request"), func() {
					s.loadPullRequest(s.selected)
				})
			})
		} else {
			detail := s.prDetail
			if strings.Contains(strings.ToLower(detail), i18n.T("no pull requests")) {
				detail = ""
			}
			ui.Text(c, i18n.T("No open pull request")).FontSize(13).FontWeight(600).TextColor(t.Text)
			ui.Text(c, i18n.T("Commit and push this branch, then open a pull request with GitHub CLI.")).
				FontSize(12).FixedLineHeight(17).TextColor(t.TextMuted).MaxLines(3)
			if detail != "" {
				faint(c, detail, 11)
			}
			ui.Row(c).Gap(6).Padding(0, 0, 4, 0).Children(func() {
				create := ui.PrimaryButton(c, "").Label(i18n.T("Create PR")).Children(func() {
					ui.Icon(c, lucide.GitPullRequest()).FontSize(15)
					ui.Text(c, i18n.T("Create PR"))
				})
				if s.gitBusy {
					create.Disabled(true)
				}
				create.OnClick(func() { s.runGitAction(proto.GitCommitPR) })
				iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh pull request"), func() {
					s.loadPullRequest(s.selected)
				})
			})
		}
	})
}

// linkedPullRequests lists the branch's pull requests.
func (s *Session) linkedPullRequests(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		surfaceHeader(c, i18n.T("%d linked", len(s.pullRequests)), func() {
			s.loadPullRequests(s.selected)
		})
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		ui.Scroll(c).Grow(1).Gap(6).Children(func() {
			if len(s.pullRequests) == 0 {
				faint(c, i18n.T("No pull requests for this branch."), 11.5)
			}
			for _, pr := range s.pullRequests {
				pr := pr
				meta := fmt.Sprintf("#%d · %s", pr.Number, strings.ToLower(pr.State))
				if pr.Draft {
					meta += i18n.T(" · draft")
				}
				ui.Column(c).Gap(3).Padding(7, 9).Radius(7).Background(t.Surface).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
						ui.Text(c, pr.Title).FontSize(12).FontWeight(600).FixedLineHeight(16).
							TextColor(t.Text).MaxLines(2).Grow(1)
						iconBtn(c, lucide.ChevronRight(), i18n.T("Details"), func() {
							s.openPullRequest(pr.Number)
						})
					})
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						faint(c, meta, 10.5)
						ui.Spacer(c)
						ui.Button(c, "").Label(i18n.T("Open")).Children(func() {
							ui.Icon(c, lucide.ExternalLink()).FontSize(14)
							ui.Text(c, i18n.T("Open"))
						}).OnClick(func() { c.OpenURL(pr.URL) })
					})
					ui.Text(c, pr.HeadBranch+" → "+pr.BaseBranch).FontSize(10.5).
						Font("monospace").TextColor(t.TextMuted).SingleLine()
				})
			}
			if detail := s.prListDetail; detail != "" &&
				!strings.Contains(strings.ToLower(detail), i18n.T("no pull requests")) {
				ui.Text(c, detail).FontSize(10.5).TextColor(t.TextMuted).MaxLines(3)
			}
		})
	})
}

// pullRequestDetail shows one pull request's checks, reviews and merge
// actions.
func (s *Session) pullRequestDetail(c *ui.Context) {
	t := c.Theme()
	detail := s.prView
	pr := detail.PullRequest
	ui.Column(c).Fill().Padding(10).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
			iconBtn(c, lucide.ChevronLeft(), i18n.T("All pull requests"), s.closePullRequest)
			text(c, fmt.Sprintf("#%d", pr.Number), 12, t.Text, 600).Grow(1)
			if s.prViewBusy {
				ui.Spinner(c).Size(13, 13).Label(i18n.T("Loading pull request"))
			}
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh pull request"), func() {
				s.openPullRequest(pr.Number)
			})
		})
		ui.Scroll(c).Grow(1).Gap(8).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, pr.Title).FontSize(13).FontWeight(600).FixedLineHeight(18).TextColor(t.Text).MaxLines(3)
				meta := strings.ToLower(pr.State)
				if pr.Draft {
					meta += i18n.T(" · draft")
				}
				if detail.Mergeable != "" {
					meta += " · " + strings.ToLower(detail.Mergeable)
				}
				if detail.ReviewDecision != "" {
					meta += " · " + strings.ToLower(detail.ReviewDecision)
				}
				faint(c, meta, 11)
				ui.Text(c, i18n.T("%s → %s · +%d −%d · %d files", pr.HeadBranch, pr.BaseBranch,
					detail.Additions, detail.Deletions, detail.ChangedFiles)).
					FontSize(10.5).Font("monospace").TextColor(t.TextMuted).MaxLines(2)
				if detail.Author != "" {
					faint(c, i18n.T("by ")+detail.Author, 11)
				}
				if detail.Body != "" {
					ui.Column(c).Padding(8).Radius(6).Background(t.Surface).Children(func() {
						ui.Text(c, detail.Body).FontSize(11).FixedLineHeight(16).TextColor(t.TextMuted).
							MaxLines(8).Selectable()
					})
				}
				if len(detail.Checks) > 0 {
					faint(c, i18n.T("Checks"), 10.5)
					ui.Column(c).Gap(2).Children(func() {
						for _, check := range detail.Checks {
							color := t.TextMuted
							switch check.Status {
							case "success":
								color = t.Success
							case "failure":
								color = t.Danger
							case "pending":
								color = t.Accent
							}
							label := check.Name
							if check.Description != "" {
								label += " · " + check.Description
							}
							ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
								ui.Icon(c, lucide.CircleCheck()).FontSize(11).TextColor(color)
								ui.Text(c, label).FontSize(10.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
							})
						}
					})
				}
				if len(detail.Reviews) > 0 {
					faint(c, i18n.T("Reviews"), 10.5)
					ui.Column(c).Gap(1).Children(func() {
						for _, review := range detail.Reviews {
							faint(c, review.Author+" · "+strings.ToLower(review.State), 10.5)
						}
					})
				}
				if len(detail.Comments) > 0 {
					faint(c, i18n.T("Comments"), 10.5)
					for _, comment := range detail.Comments {
						label := comment.Author
						if comment.CreatedAt != "" {
							label += " · " + comment.CreatedAt
						}
						ui.Column(c).Gap(2).Padding(7).Radius(6).Background(t.Surface).Children(func() {
							ui.Text(c, label).FontSize(10).TextColor(t.TextMuted).SingleLine()
							ui.Text(c, comment.Body).FontSize(11).FixedLineHeight(16).TextColor(t.Text).
								MaxLines(6).Selectable()
						})
					}
				}
				if strings.EqualFold(pr.State, "open") {
					faint(c, i18n.T("Review or comment"), 10.5)
					ui.TextArea(c, &s.prNote).Label(i18n.T("Note")).
						Placeholder(i18n.T("Leave a note…")).Lines(2, 6)
					ui.Row(c).Wrap().Gap(4).Children(func() {
						ui.PrimaryButton(c, "").Label(i18n.T("Approve")).Children(func() {
							ui.Icon(c, lucide.CircleCheck()).FontSize(15)
							ui.Text(c, i18n.T("Approve"))
						}).OnClick(func() { s.submitPRReview("approve") })
						ui.Button(c, i18n.T("Request changes")).OnClick(func() {
							s.submitPRReview("request-changes")
						})
						ui.Button(c, "").Label(i18n.T("Add comment")).Children(func() {
							ui.Icon(c, lucide.MessageCircle()).FontSize(15)
							ui.Text(c, i18n.T("Add comment"))
						}).OnClick(s.submitPRComment)
					})
				}
				ui.Row(c).Wrap().Gap(4).Padding(0, 0, 4, 0).Children(func() {
					if pr.URL != "" {
						ui.Button(c, "").Label(i18n.T("Open on GitHub")).Children(func() {
							ui.Icon(c, lucide.ExternalLink()).FontSize(15)
							ui.Text(c, i18n.T("Open on GitHub"))
						}).OnClick(func() { c.OpenURL(pr.URL) })
					}
					if strings.EqualFold(pr.State, "open") {
						if pr.Draft {
							ui.Button(c, i18n.T("Ready")).OnClick(func() {
								s.changePullRequestState("ready")
							})
						} else if detail.Mergeable == "MERGEABLE" || detail.Mergeable == "" {
							for _, method := range []string{"merge", "squash", "rebase"} {
								method := method
								ui.PrimaryButton(c, strings.Title(method)).OnClick(func() {
									s.mergePullRequest(method)
								})
							}
						}
						ui.Button(c, i18n.T("Close")).OnClick(func() {
							s.changePullRequestState("close")
						})
					} else {
						ui.Button(c, i18n.T("Reopen")).OnClick(func() {
							s.changePullRequestState("reopen")
						})
					}
				})
				if strings.EqualFold(pr.State, "open") {
					deleteLabel := i18n.T("Keep branch")
					if s.prDeleteBranch {
						deleteLabel = i18n.T("Delete branch")
					}
					ui.Button(c, deleteLabel).OnClick(func() {
						s.prDeleteBranch = !s.prDeleteBranch
					})
				}
			})
		})
	})
}

// sideChatSurface asks questions in a hidden thread with the current
// one's context, without touching its transcript.
func (s *Session) sideChatSurface(c *ui.Context) {
	t := c.Theme()
	if s.selected == "" {
		surfaceNotice(c, i18n.T("Pick a thread to ask a side question."))
		return
	}
	thread := s.sideChat
	if thread.ID == "" {
		note := i18n.T("A side chat keeps this thread's conversation out of the way.")
		if s.sideErr != "" {
			note = s.sideErr
		}
		ui.Column(c).Grow(1).Center().Padding(18).Gap(8).Children(func() {
			ui.Icon(c, lucide.MessageCircleMore()).FontSize(22).TextColor(t.TextMuted)
			ui.Text(c, note).FontSize(11.5).TextColor(t.TextMuted).MaxLines(4).TextAlign(ui.Center)
			if s.sideBusy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14).Label(i18n.T("Starting side chat"))
				})
			} else {
				ui.PrimaryButton(c, "").Label(i18n.T("Start side chat")).Children(func() {
					ui.Icon(c, lucide.MessageCircleMore()).FontSize(15)
					ui.Text(c, i18n.T("Start side chat"))
				}).OnClick(s.openSideChat)
			}
		})
		return
	}
	ui.Column(c).Fill().Padding(10).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			text(c, i18n.T("Side chat"), 12, t.Text, 600).Grow(1)
			if s.sideBusy {
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Working on the side chat"))
			}
			ui.Button(c, i18n.T("Reset")).OnClick(func() {
				s.resetSideChat()
				s.sideFor = ""
				s.openSideChat()
			})
		})
		if s.sideErr != "" {
			ui.Text(c, s.sideErr).FontSize(10.5).TextColor(t.Danger).MaxLines(3)
		}
		ui.Scroll(c).Grow(1).Gap(8).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				if len(s.sideMessages) == 0 {
					ui.Text(c, i18n.T("Ask a question about this thread.")).
						FontSize(11).TextColor(t.TextMuted).MaxLines(2)
				}
				for _, message := range s.sideMessages {
					message := message
					switch message.Kind {
					case proto.KindText:
						if message.Role == proto.RoleUser {
							ui.Row(c).Justify(ui.End).Children(func() {
								ui.Column(c).MaxWidthPercent(88).Padding(6, 9).Radius(8).
									Background(t.Surface).Children(func() {
									ui.Text(c, message.Content).FontSize(11.5).FixedLineHeight(16).
										TextColor(t.Text).Selectable()
								})
							})
						} else {
							markdown.View(c, s.markdownPreview(message.Content, ""), &s.md)
						}
					case proto.KindReasoning:
						ui.Text(c, message.Content).FontSize(10.5).Italic().
							TextColor(t.TextMuted).MaxLines(4)
					case proto.KindTool:
						label := message.Title
						if label == "" {
							label = "Tool"
						}
						ui.Text(c, label).FontSize(10.5).TextColor(t.TextMuted).MaxLines(2)
					case proto.KindError:
						ui.Text(c, message.Content).FontSize(10.5).TextColor(t.Danger).MaxLines(5)
					case proto.KindNotice:
						ui.Text(c, message.Content).FontSize(10).TextColor(t.TextMuted).MaxLines(2)
					}
				}
			})
		})
		// The question field and its choices.
		ui.Column(c).Gap(5).Padding(6, 0, 0, 0).Children(func() {
			s.sideAttachmentChips(c)
			ui.TextArea(c, &s.sideDraft).Label(i18n.T("Side question")).
				Placeholder(i18n.T("Ask a side question…")).Lines(1, 6)
			ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
				modelButton := ui.Button(c, "").Label(i18n.T("Side chat model")).Children(func() {
					ui.Text(c, shortModel(thread.Model)).FontSize(10.5).TextColor(t.TextMuted).SingleLine().MaxWidth(140)
				})
				s.modelPicker(c, modelButton, thread.Provider, thread.Model, s.setSideModel)
				s.configOptions(c, []configOption{s.sideChatPermissionOption(thread)})
				ui.Spacer(c)
				iconBtn(c, lucide.Paperclip(), i18n.T("Attach files"), s.pickSideAttachments)
				if s.sideRunning {
					ui.Button(c, i18n.T("Stop")).OnClick(s.stopSideChat)
				} else {
					send := ui.PrimaryButton(c, "").Label(i18n.T("Send")).Children(func() {
						ui.Icon(c, lucide.Send()).FontSize(15)
						ui.Text(c, i18n.T("Send"))
					})
					if strings.TrimSpace(s.sideDraft) == "" {
						send.Disabled(true)
					}
					send.OnClick(s.sendSideChat)
				}
			})
		})
	})
	if s.selected != "" && len(s.sideAttachments) > 0 {
		// Dropped files join the side chat's attachments.
	}
}

// sideAttachmentChips are the files the side chat's next question carries.
func (s *Session) sideAttachmentChips(c *ui.Context) {
	t := c.Theme()
	if len(s.sideAttachments) == 0 {
		return
	}
	ui.Column(c).Gap(3).Children(func() {
		for _, file := range s.sideAttachments {
			file := file
			ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(3, 6).Radius(6).
				Background(t.Surface).Children(func() {
				ui.Icon(c, lucide.Paperclip()).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, file.Name).FontSize(10.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
				faint(c, humanSize(file.Size), 9.5)
				iconBtn(c, lucide.X(), i18n.T("Remove "+file.Name), func() {
					s.removeSideAttachment(file.ID)
				})
			})
		}
	})
}

// previewSurface lists the local development servers to open.
func (s *Session) previewSurface(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			text(c, i18n.T("Local servers · %d", len(s.previewServers)), 12, t.Text, 600).Grow(1)
			if s.previewBusy {
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Looking for local servers"))
			}
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh local servers"), s.loadPreviewServers)
		})
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		if s.previewErr != "" {
			ui.Text(c, s.previewErr).FontSize(10.5).TextColor(t.Danger).MaxLines(3)
		}
		ui.Scroll(c).Grow(1).Gap(5).Children(func() {
			if len(s.previewServers) == 0 {
				detail := s.previewDetail
				if detail == "" {
					detail = i18n.T("No local servers found. Start a dev server, then refresh.")
				}
				ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted).MaxLines(5)
			}
			for _, server := range s.previewServers {
				server := server
				name := server.Process
				if name == "" {
					name = i18n.T("Server")
				}
				ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 8).Radius(7).
					Background(t.Surface).Children(func() {
					ui.Icon(c, lucide.MonitorPlay()).FontSize(14).TextColor(t.TextMuted)
					ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, fmt.Sprintf("%s · %d", name, server.Port)).FontSize(11.5).
							FontWeight(500).TextColor(t.Text).SingleLine()
						ui.Text(c, server.URL).FontSize(10).Font("monospace").
							TextColor(t.TextMuted).SingleLine()
					})
					ui.Button(c, "").Label(i18n.T("Open")).Children(func() {
						ui.Icon(c, lucide.ExternalLink()).FontSize(14)
						ui.Text(c, i18n.T("Open"))
					}).OnClick(func() { s.openPreviewServer(server.URL) })
				})
			}
		})
	})
}

// deviceSurface lists the environment's simulators and shows one screen.
func (s *Session) deviceSurface(c *ui.Context) {
	t := c.Theme()
	devices := s.devices
	var selected proto.Device
	for _, device := range devices {
		if device.ID == s.deviceSelected {
			selected = device
		}
	}
	selector := i18n.T("Choose a device")
	if selected.Name != "" {
		selector = selected.Name
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(4, 10).Children(func() {
			ui.Button(c, "").Label("").Label(i18n.T("Devices")).Children(func() {
				ui.Icon(c, lucide.MonitorSmartphone()).FontSize(14)
				ui.Text(c, selector).SingleLine().MaxWidth(180)
			}).OnClick(func() {
				s.showDevices = true
				s.loadDevices()
			})
			if selected.Name != "" {
				faint(c, strings.ToLower(selected.State), 10)
			}
			ui.Spacer(c)
			if s.deviceBusy {
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Working with the simulator"))
			}
			liveIcon, liveLabel := lucide.Play(), i18n.T("Resume live screen")
			if s.deviceLive {
				liveIcon, liveLabel = lucide.Pause(), i18n.T("Pause live screen")
			}
			iconBtn(c, liveIcon, liveLabel, func() { s.setDeviceLive(!s.deviceLive) })
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh devices"), s.loadDevices)
		})
		if s.deviceErr != "" {
			ui.Text(c, s.deviceErr).FontSize(10.5).TextColor(t.Danger).MaxLines(3).Padding(0, 10, 4, 10)
		}
		switch {
		case len(devices) == 0:
			detail := s.deviceDetail
			if detail == "" {
				detail = i18n.T("No simulators found.")
			}
			ui.Column(c).Grow(1).Center().Padding(16).Children(func() {
				ui.Text(c, detail).FontSize(11.5).TextColor(t.TextMuted).MaxLines(6).TextAlign(ui.Center)
			})
		case selected.ID == "":
			ui.Column(c).Grow(1).Center().Children(func() {
				faint(c, i18n.T("Choose a simulator above."), 11.5)
			})
		case strings.ToLower(selected.State) == "booted":
			ui.Column(c).Grow(1).MinHeight(0).Padding(2, 10, 10, 10).Gap(6).Children(func() {
				ui.Box(c).Grow(1).FillWidth().Radius(8).Background(t.Surface).Clip().Children(func() {
					bitmap := s.deviceBitmap()
					if bitmap == nil {
						ui.Column(c).Fill().Center().Children(func() {
							faint(c, i18n.T("No screenshot yet."), 11.5)
						})
						return
					}
					ui.Image(c, bitmap).Fit(ui.Contain).Fill()
				})
				ui.Row(c).Gap(4).Children(func() {
					ui.Button(c, "").Label(i18n.T("Refresh screen")).Children(func() {
						ui.Icon(c, lucide.RefreshCw()).FontSize(15)
						ui.Text(c, i18n.T("Refresh screen"))
					}).OnClick(s.refreshDeviceShot)
					ui.Spacer(c)
					ui.Button(c, "").Label(i18n.T("Shut down")).Children(func() {
						ui.Icon(c, lucide.Power()).FontSize(15)
						ui.Text(c, i18n.T("Shut down"))
					}).OnClick(func() { s.shutdownDevice(selected.ID) })
				})
			})
		default:
			ui.Column(c).Grow(1).Center().Padding(16).Gap(6).Children(func() {
				ui.Text(c, selected.Name).FontSize(12.5).FontWeight(600).TextColor(t.Text).SingleLine()
				faint(c, selected.OS+" · "+strings.ToLower(selected.State), 10.5)
				ui.PrimaryButton(c, "").Label(i18n.T("Boot")).Children(func() {
					ui.Icon(c, lucide.Play()).FontSize(15)
					ui.Text(c, i18n.T("Boot"))
				}).OnClick(func() { s.bootDevice(selected.ID) })
			})
		}
	})
}

// browserSurface shows a web page in the window's web view, with its
// address bar. A web client has no web view to show.
func (s *Session) browserSurface(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6, 10).Children(func() {
			page := s.browserWebView()
			iconBtn(c, lucide.ChevronLeft(), i18n.T("Back"), func() { page.GoBack() })
			iconBtn(c, lucide.ChevronRight(), i18n.T("Forward"), func() { page.GoForward() })
			iconBtn(c, lucide.RefreshCw(), i18n.T("Reload"), func() { page.Reload() })
			ui.TextInput(c, &s.browserInput).Label(i18n.T("Address")).
				Placeholder("https://example.com").Grow(1).MinHeight(controlMinHeight(c))
			iconBtn(c, lucide.ArrowRight(), i18n.T("Go"), func() { s.goBrowser(s.browserInput) })
		})
		if s.browserNote != "" {
			ui.Text(c, s.browserNote).FontSize(11.5).TextColor(t.Danger).MaxLines(2).Padding(6, 10)
		}
		if s.browserWeb == nil {
			ui.Column(c).Grow(1).Center().Padding(20).Gap(8).Children(func() {
				ui.Icon(c, lucide.Globe()).FontSize(28).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("Open a page")).FontSize(13).FontWeight(600).TextColor(t.Text)
				faint(c, i18n.T("Type an address above, or open a local dev server."), 11.5)
				ui.Column(c).Gap(2).AlignItems(ui.Center).Padding(6, 0, 0, 0).Children(func() {
					for _, link := range []string{"https://go.dev", "http://localhost:3000", "http://localhost:5173"} {
						link := link
						ui.Button(c, link).OnClick(func() { s.goBrowser(link) })
					}
				})
			})
			return
		}
		ui.WebView(c, s.browserWeb).Grow(1).FillWidth()
	})
}

// browserWebView returns the page the browser surface shows, opening the
// web view the first time it is needed. A web client has none, and a
// test's headless window does either.
func (s *Session) browserWebView() *mygo.Page {
	if s.browserWeb == nil && !jsRuntime && s.win != nil {
		view, err := s.win.NewWebView(mygo.WebViewOptions{URL: s.browserURL})
		if err != nil {
			s.browserNote = friendlyError(err)
			return nil
		}
		s.browserWeb = view
		page := view.Page()
		page.OnDidNavigate(func(url string) {
			s.post(func() {
				s.browserInput = url
				s.browserBack, s.browserNext = page.CanGoBack(), page.CanGoForward()
			})
		})
	}
	if s.browserWeb == nil {
		return nil
	}
	return s.browserWeb.Page()
}

// graphSurface draws the repository's commits beside their revision
// graph: a lane for every branch the history takes, with the commit
// that owns each of them.
func (s *Session) graphSurface(c *ui.Context) {
	t := c.Theme()
	if s.selected == "" {
		surfaceNotice(c, i18n.T("Pick a thread to see its commit graph."))
		return
	}
	commits := s.graph
	query := strings.ToLower(strings.TrimSpace(s.graphQuery))
	if query != "" {
		filtered := make([]proto.GraphCommit, 0, len(commits))
		for _, commit := range commits {
			if strings.Contains(strings.ToLower(commit.Subject), query) ||
				strings.Contains(strings.ToLower(commit.Hash), query) ||
				strings.Contains(strings.ToLower(commit.Author), query) {
				filtered = append(filtered, commit)
			}
		}
		commits = filtered
	}
	rows, lanes := graphLanes(commits)
	wide := max(s.rightPanelWidth(c), 280) >= 520
	ui.Column(c).Fill().Padding(10).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.SearchField(c, &s.graphQuery).Label(i18n.T("Search commits")).
				Placeholder(i18n.T("Text or hash")).Grow(1).MinHeight(controlMinHeight(c))
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh commits"), func() { s.loadGraph(s.selected) })
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			faint(c, i18n.T("%d commits", len(commits)), 10.5)
			ui.Spacer(c)
			if query != "" {
				faint(c, i18n.T("Filtered"), 10.5)
			}
		})
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		if len(commits) == 0 {
			if len(s.graph) == 0 {
				faint(c, i18n.T("No commits yet."), 12)
			} else {
				faint(c, i18n.T("No commits match."), 12)
			}
			return
		}
		ui.Scroll(c).Grow(1).Gap(0).Children(func() {
			for i, commit := range commits {
				commit := commit
				merge := len(commit.Parents) > 1
				color := t.Text
				if merge {
					color = t.TextMuted
				}
				row := clickableRow(c, commit.Subject, func() {
					graphLaneGutter(c, rows[i], lanes)
					if wide {
						ui.Text(c, commit.Subject).FontSize(12).TextColor(color).SingleLine().Grow(1).MinWidth(0)
						graphRefChips(c, commit)
						faint(c, commit.Author, 10.5)
						faint(c, graphCommitDate(commit.Date), 10.5)
						return
					}
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
							ui.Text(c, commit.Subject).FontSize(12).TextColor(color).SingleLine().Grow(1).MinWidth(0)
							graphRefChips(c, commit)
						})
						ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
							faint(c, commit.Author, 10)
							faint(c, "·", 10)
							faint(c, graphCommitDate(commit.Date), 10)
						})
					})
				}).Gap(8).Padding(0, 6).MinHeight(34).Radius(6).
					Tooltip(commit.Subject + " · " + commit.Short + " · " + i18n.T("Double-click for the diff"))
				if s.graphSelected == commit.Hash {
					row.Background(t.Surface)
				} else if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				if row.Clicked() {
					s.graphSelected = commit.Hash
				}
				if row.DoubleClicked() {
					s.openCommitDiffTab(commit.Hash, commit.Subject, commit.Short)
				}
			}
		})
	})
}

// graphRefChips draws the branches and tags that name a commit.
func graphRefChips(c *ui.Context, commit proto.GraphCommit) {
	t := c.Theme()
	for _, ref := range commit.Refs {
		ref := ref
		name := ref.Name
		if ref.Current {
			name = name + " ·" + i18n.T("current")
		}
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(1, 5).Radius(5).
			Background(t.Surface).Shrink(0).Children(func() {
			ui.Icon(c, lucide.GitBranch()).FontSize(9).TextColor(t.TextMuted)
			ui.Text(c, name).FontSize(9.5).TextColor(t.TextMuted).SingleLine().MaxWidth(120)
		})
	}
}

// graphCommitDate writes a commit's date as the graph lists it.
func graphCommitDate(date time.Time) string {
	if date.IsZero() {
		return ""
	}
	return date.Format("2006/1/2 15:04")
}

// rightPanelWidth is the surfaces panel's width, the room the graph has.
func (s *Session) rightPanelWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	_, _, panel := s.desktopPaneWidths(width)
	return panel
}

// graphLane is one branch of the history the graph draws: the commit it
// waits for, the row it starts at, and the row it ends at.
type graphLane struct {
	expect string
	start  int
	end    int
	// origin is the lane of the commit that opened this lane as another
	// parent, so the graph can draw the diagonal that joins them.
	origin int
}

// graphRow is one commit of the graph: the lane it belongs to, and the
// lanes that pass it.
type graphRow struct {
	lane     int
	lanes    []bool
	incoming []graphIncoming
}

// graphIncoming is a lane joined from the row above: from the commit's
// lane to the lane it opened.
type graphIncoming struct {
	from, to int
}

// graphLanes lays the commits out in lanes: a commit takes the lane that
// waited for it, and its other parents open lanes of their own.
func graphLanes(commits []proto.GraphCommit) ([]graphRow, int) {
	var lanes []graphLane
	rows := make([]graphRow, 0, len(commits))
	for index, commit := range commits {
		own := -1
		for i, lane := range lanes {
			if lane.expect == commit.Hash {
				own = i
				break
			}
		}
		if own < 0 {
			lanes = append(lanes, graphLane{expect: commit.Hash, start: index, end: -1})
			own = len(lanes) - 1
		}
		row := graphRow{lane: own}
		for _, lane := range lanes {
			row.lanes = append(row.lanes, lane.start <= index && (lane.end < 0 || lane.end >= index))
		}
		rows = append(rows, row)
		// The lane the commit sits in waits for its first parent; the
		// others open lanes of their own.
		if len(commit.Parents) > 0 {
			lanes[own].expect = commit.Parents[0]
			for _, parent := range commit.Parents[1:] {
				lanes = append(lanes, graphLane{expect: parent, start: index + 1, end: -1, origin: own})
			}
		} else {
			lanes[own].expect = ""
			lanes[own].end = index
		}
	}
	// The lanes a row names may be fewer than the lanes the graph has:
	// a lane that starts later has no place in the rows before it.
	for i := range rows {
		for len(rows[i].lanes) < len(lanes) {
			rows[i].lanes = append(rows[i].lanes, false)
		}
	}
	// A lane opened for another parent joins the row below from the
	// commit that opened it.
	for i, lane := range lanes {
		if lane.start > 0 && lane.start < len(rows) {
			rows[lane.start].incoming = append(rows[lane.start].incoming, graphIncoming{from: lane.origin, to: i})
		}
	}
	return rows, len(lanes)
}

// graphLaneGutter draws the lanes of one commit: a dot in the lane the
// commit owns, lines in the lanes that pass it, and diagonals joining the
// lanes a merge opens below.
func graphLaneGutter(c *ui.Context, row graphRow, lanes int) {
	t := c.Theme()
	const laneW = 12
	const height = 34
	colors := []ui.Color{t.Accent, t.Success, t.Warning, t.Danger, t.TextMuted}
	ui.Box(c).Width(float32(lanes*laneW) + 4).Height(height).Shrink(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			center := func(i int) float32 { return r.X + 2 + float32(i)*laneW + laneW/2 }
			color := func(i int) ui.Color { return colors[i%len(colors)] }
			mid := r.Y + r.H/2
			top, bottom := r.Y-1, r.Y+r.H+1
			started := map[int]bool{}
			for _, link := range row.incoming {
				started[link.to] = true
				graphSlant(p, center(link.from), top, center(link.to), mid, 2, color(link.to))
				p.Line(center(link.to), mid, center(link.to), bottom, 2, color(link.to))
			}
			for i := 0; i < lanes; i++ {
				if started[i] {
					continue
				}
				if i == row.lane {
					p.Line(center(i), top, center(i), bottom, 2, color(i))
					continue
				}
				if i < len(row.lanes) && row.lanes[i] {
					p.Line(center(i), top, center(i), bottom, 2, color(i).Alpha(0.7))
				}
			}
			p.Fill(ui.Rect{X: center(row.lane) - 4, Y: mid - 4, W: 8, H: 8}, color(row.lane), 4)
		})
}

// graphSlant draws a diagonal, which Painter.Line cannot: small squares
// follow the segment so a branch visibly joins the lane it opens.
func graphSlant(p *ui.Painter, x0, y0, x1, y1, width float32, c ui.Color) {
	dx, dy := x1-x0, y1-y0
	steps := int(max(absf(dx), absf(dy)))
	if steps < 1 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		x, y := x0+dx*t, y0+dy*t
		p.Fill(ui.Rect{X: x - width/2, Y: y - width/2, W: width, H: width}, c, width/2)
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// jsRuntime reports a browser build, where web views and the terminal
// plugin do not exist.
var jsRuntime = runtime.GOOS == "js" && runtime.GOARCH == "wasm"
