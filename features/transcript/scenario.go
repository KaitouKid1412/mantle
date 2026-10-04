package transcript

// sampleSession is a sanitized engine transcript (stream-json stdout) that
// exercises every renderer. Stories, goldens and replay tests use it.
const sampleSession = `
{"type":"system","subtype":"init","uuid":"i0","cwd":"/work/app","tools":[],"mcp_servers":[],"model":"m","permissionMode":"default","slash_commands":[],"claude_code_version":"2.1.288","output_style":"default"}
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":[{"type":"text","text":"The worker retries too fast. Can you look at internal/queue and fix the backoff?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}}
{"type":"system","subtype":"session_state_changed","state":"running"}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"thinking","thinking":"The retry path probably re-enqueues immediately. I should read worker.go first.","signature":"s"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m1","content":[{"type":"text","text":"I'll start by reading the worker and searching for the retry path."}]}}
{"type":"assistant","uuid":"a3","message":{"id":"m1","content":[{"type":"tool_use","id":"t_read","name":"Read","input":{"file_path":"/work/app/internal/queue/worker.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_read","content":"package queue\n..."}]},"tool_use_result":{"type":"text","file":{"filePath":"/work/app/internal/queue/worker.go","content":"package queue\n\nfunc (w *Worker) handle(job *Job) {\n\tw.enqueue(job)\n}\n","numLines":5,"startLine":1,"totalLines":5}}}
{"type":"assistant","uuid":"a4","message":{"id":"m1","content":[{"type":"tool_use","id":"t_grep","name":"Grep","input":{"pattern":"enqueue\\(","path":"/work/app/internal","output_mode":"files_with_matches"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_grep","content":"Found 2 files"}]},"tool_use_result":{"mode":"files_with_matches","filenames":["/work/app/internal/queue/worker.go","/work/app/internal/queue/queue.go"],"numFiles":2}}
{"type":"assistant","uuid":"a5","message":{"id":"m1","content":[{"type":"tool_use","id":"t_glob","name":"Glob","input":{"pattern":"**/*_test.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_glob","content":"a_test.go"}]},"tool_use_result":{"filenames":["internal/queue/worker_test.go"],"numFiles":1,"durationMs":3,"truncated":false}}
{"type":"assistant","uuid":"a6","message":{"id":"m1","content":[{"type":"tool_use","id":"t_bash","name":"Bash","input":{"command":"go test ./internal/queue/... -run TestRetry -count=1","description":"Run the retry tests"}}]}}
{"type":"tool_progress","tool_use_id":"t_bash","tool_name":"Bash","elapsed_time_seconds":3}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_bash","content":"FAIL"}]},"tool_use_result":{"stdout":"=== RUN   TestRetryBackoff\n    worker_test.go:41: retried after 0s, want >= 200ms\n--- FAIL: TestRetryBackoff (0.00s)\n=== RUN   TestRetryLimit\n--- PASS: TestRetryLimit (0.00s)\nFAIL\nFAIL\tgithub.com/example/app/internal/queue\t0.012s\nFAIL","stderr":"","interrupted":false,"isImage":false}}
{"type":"assistant","uuid":"a7","message":{"id":"m1","content":[{"type":"tool_use","id":"t_edit","name":"Edit","input":{"file_path":"/work/app/internal/queue/worker.go","old_string":"\tw.enqueue(job)","new_string":"\tw.enqueueAfter(job, backoff(job.Attempts))"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_edit","content":"ok"}]},"tool_use_result":{"filePath":"/work/app/internal/queue/worker.go","oldString":"\tw.enqueue(job)","newString":"\tw.enqueueAfter(job, backoff(job.Attempts))","structuredPatch":[{"oldStart":2,"oldLines":4,"newStart":2,"newLines":4,"lines":[" "," func (w *Worker) handle(job *Job) {","-\tw.enqueue(job)","+\tw.enqueueAfter(job, backoff(job.Attempts))"," }"]}],"userModified":false,"replaceAll":false}}
{"type":"assistant","uuid":"a8","message":{"id":"m1","content":[{"type":"tool_use","id":"t_write","name":"Write","input":{"file_path":"/work/app/internal/queue/backoff.go","content":"package queue\n\nimport \"time\"\n\n// backoff returns the delay before retry attempt n.\nfunc backoff(n int) time.Duration {\n\treturn 200 * time.Millisecond << min(n, 8)\n}\n"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_write","content":"File created"}]},"tool_use_result":{"type":"create","filePath":"/work/app/internal/queue/backoff.go","content":"package queue\n","structuredPatch":[]}}
{"type":"assistant","uuid":"a9","message":{"id":"m1","content":[{"type":"tool_use","id":"t_todo","name":"TodoWrite","input":{"todos":[{"content":"Read the worker","status":"completed","activeForm":"Reading"},{"content":"Add exponential backoff","status":"in_progress","activeForm":"Adding backoff"},{"content":"Run the tests","status":"pending","activeForm":"Running tests"}]}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_todo","content":"ok"}]}}
{"type":"assistant","uuid":"a10","message":{"id":"m1","content":[{"type":"tool_use","id":"t_agent","name":"Agent","input":{"description":"Find other retry loops","prompt":"Search the repo for retry loops without backoff.","subagent_type":"Explore"}}]}}
{"type":"system","subtype":"task_started","task_id":"k1","tool_use_id":"t_agent","description":"Find other retry loops","subagent_type":"Explore"}
{"type":"assistant","uuid":"a11","parent_tool_use_id":"t_agent","message":{"id":"m2","content":[{"type":"tool_use","id":"t_sub1","name":"Grep","input":{"pattern":"retry","path":"/work/app"}}]}}
{"type":"user","parent_tool_use_id":"t_agent","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_sub1","content":"3 files"}]},"tool_use_result":{"mode":"files_with_matches","filenames":["a.go","b.go","c.go"],"numFiles":3}}
{"type":"assistant","uuid":"a12","parent_tool_use_id":"t_agent","message":{"id":"m2","content":[{"type":"tool_use","id":"t_sub2","name":"Read","input":{"file_path":"/work/app/internal/sync/client.go","offset":40,"limit":30}}]}}
{"type":"user","parent_tool_use_id":"t_agent","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_sub2","content":"..."}]}}
{"type":"system","subtype":"task_notification","task_id":"k1","status":"completed","summary":"One more loop in internal/sync","usage":{"total_tokens":18400,"tool_uses":2,"duration_ms":14000}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_agent","content":[{"type":"text","text":"internal/sync/client.go also retries without delay (line 52)."}]}]},"tool_use_result":{"status":"completed","totalDurationMs":14000,"totalTokens":18400,"totalToolUseCount":2}}
{"type":"assistant","uuid":"a13","message":{"id":"m1","content":[{"type":"tool_use","id":"t_mcp1","name":"mcp__tracker__get_issue","input":{"id":"APP-12"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_mcp1","content":"APP-12: Worker retries too fast"}]}}
{"type":"assistant","uuid":"a14","message":{"id":"m1","content":[{"type":"tool_use","id":"t_mcp2","name":"mcp__tracker__add_comment","input":{"id":"APP-12","body":"Fix in progress"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_mcp2","content":"ok"}]}}
{"type":"assistant","uuid":"a15","message":{"id":"m1","content":[{"type":"tool_use","id":"t_fetch","name":"WebFetch","input":{"url":"https://example.com/docs/backoff","prompt":"Summarise the jitter advice"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_fetch","content":"Use full jitter."}]},"tool_use_result":{"bytes":18230,"code":200,"codeText":"OK","result":"Use full jitter.","durationMs":800,"url":"https://example.com/docs/backoff"}}
{"type":"assistant","uuid":"a16","message":{"id":"m1","content":[{"type":"tool_use","id":"t_bad","name":"Read","input":{"file_path":"/work/app/missing.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_bad","content":"<tool_use_error>File does not exist.</tool_use_error>","is_error":true}]}}
{"type":"assistant","uuid":"a17","message":{"id":"m1","content":[{"type":"tool_use","id":"t_ask","name":"AskUserQuestion","input":{"questions":[{"question":"Fix internal/sync too?","header":"Scope","options":[{"label":"Yes"},{"label":"No"}],"multiSelect":false}]}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t_ask","content":"answered"}]},"tool_use_result":{"questions":[],"answers":{"Fix internal/sync too?":"Yes"}}}
{"type":"system","subtype":"informational","uuid":"inf1","content":"Context is 80% full; consider /compact.","level":"warning"}
{"type":"assistant","uuid":"a18","message":{"id":"m1","content":[{"type":"text","text":"Fixed. The worker now waits before retrying:\n\n- **Backoff** starts at 200ms and doubles per attempt, capped at 8 doublings.\n- The failing test \u0060TestRetryBackoff\u0060 now passes.\n\n| File | Change |\n|---|---|\n| \u0060worker.go\u0060 | uses \u0060enqueueAfter\u0060 |\n| \u0060backoff.go\u0060 | new helper |\n\nI also found the same issue in \u0060internal/sync/client.go\u0060; I can fix that next."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":66000,"duration_api_ms":30000,"is_error":false,"num_turns":12,"result":"Fixed.","total_cost_usd":0.12,"permission_denials":[{"tool_name":"Bash","tool_use_id":"t_x","tool_input":{"command":"rm -rf build"}}]}
{"type":"system","subtype":"session_state_changed","state":"idle"}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"manual","pre_tokens":48200,"post_tokens":7900}}
{"type":"assistant","uuid":"a19","error":"rate_limit","message":{"id":"m3","model":"<synthetic>","content":[{"type":"text","text":"API Error: 429 rate limited"}]}}
{"type":"assistant","uuid":"a20","local_command_run":{"command":"context","args":""},"message":{"id":"m4","model":"<synthetic>","content":[{"type":"text","text":"Context usage: 7.9k / 200k tokens (4%)"}]}}
{"type":"result","subtype":"success","uuid":"r9","duration_ms":30,"is_error":false,"num_turns":0,"local_command":"context","total_cost_usd":0}
`
