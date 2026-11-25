Persona 算好的事实（这次开跑时的快照，数字以此为准，别改也别另编）：
{{with .NorthStar}}- 北极星指标（报告开头先照这几行说：今天的打扰次数和可用率、和昨天同一时段比的变化、数据来源，再接下面的内容；数字原样，未知就说未知）：{{range .}}
  - {{.}}{{end}}
{{end}}{{if .Card}}这些会由 Persona 原样列在发给用户的飞书卡片里（含链接和「通过」按钮），你不用再逐条列。{{else}}这次不会发飞书卡片（网页版里有同样的条目，手机 App 里没有）：请在一两句总述之后，按下面的顺序把它们每件一行列出来，说清要用户做什么、去哪点。{{end}}
{{if .WaitingUnknown}}- 等用户处理的：刚重启，还没读到，不要说「没有」。{{else}}- 等用户处理的 {{.WaitingTotal}} 件{{range .Waiting}}
  - {{.}}{{end}}{{end}}
- 过去 {{.Hours}} 小时没成 {{.FailedTotal}} 件{{range .Failed}}
  - {{.Title}}（{{if .Skipped}}被跳过{{else if .Run}}运行失败{{else}}没开起来{{end}}{{if .Why}}：{{.Why}}{{end}}）{{end}}
- 过去 {{.Hours}} 小时做完 {{.DoneTotal}} 件{{range .Done}}
  - {{.}}{{end}}
- 等用户验收的 {{.AcceptTotal}} 件{{range .Accept}}
  - {{.Line}}{{if .Changed}}（改了：{{.Changed}}）{{end}}{{end}}
{{if .LaterTotal}}- 可以晚点答的问题 {{.LaterTotal}} 件：Agent 留下的、不急的问题，列在早晚报的「可以晚点答」一节里。只说有几件，不要逐条再列，也不要替用户回答。
{{end}}- 正在做的 {{.ProgressTotal}} 件{{range .Progress}}
  - {{.Title}}{{if .Stage}}（阶段：{{.Stage}}）{{end}}{{end}}{{with .InboxWaiting}}
- 收件箱里等对方超过一天的（只给你看，卡片、简报条目和推送里没有；即使上面有卡片，也要在回答里逐条提）：{{range .}}
  - {{.Title}}{{with .Who}}；在等：{{.}}{{end}}{{with .Next}}；下一步：{{.}}{{end}}{{end}}{{end}}{{with .InboxDue}}
- 收件箱里今天到点的（含已经到点但还没回看的，以及看过 3 次没动静或到了再看时间的；同样只给你看，要在回答里逐条提）：{{range .}}
  - {{.Title}}{{with .Who}}；在等：{{.}}{{end}}{{with .Next}}；下一步：{{.}}{{end}}{{end}}{{end}}{{with .Directions}}
- 停着的事（这些是 Persona 按任务账本、会话和卡片算好的，不要漏掉、不要改数字；等用户的行优先，owner 行全部列出，非 owner 行折叠时会写「另 N 件」）：{{range .}}
  - {{.Name}}：在办 {{.Open}} 件，活动 {{.Active}} 件，未派发 {{.Undispatched}} 件{{range .Waits}}
    - {{.Line}}{{with .Link}} {{.}}{{end}}{{end}}{{if .More}}
    - 另 {{.More}} 件，打开该方向查看：{{.Link}}{{end}}{{end}}{{end}}{{with .DisabledRoutines}}
- 已关闭的每日收尾和巡检（每个一行；没记到关闭时间的必须照写「关闭时间未记录」，不能拿最后运行时间代替）：{{range .}}
  - {{.Line}}{{end}}{{end}}{{with .Briefs}}
- 派出去的事里要说的（用用户当初的原话开头，每件一行；没列出来的不需要用户做什么、也没有变化，不要提）：{{range .}}
  - {{if .Words}}{{.Date}} 你说：「{{.Words}}」{{else}}{{.Date}} {{if eq .By "routine"}}定时任务{{else if eq .By "hub"}}Persona{{else if eq .By "unknown"}}（没记下是谁要的）{{else}}助手{{end}}发起：{{.Goal}}{{end}} → {{.Now}}{{with .Status}}（Agent 自己的说法：{{if eq . "done"}}做完了{{else if eq . "blocked"}}卡住了{{else}}没做完{{end}}）{{end}}{{with .Need}}；要用户：{{.}}{{end}}{{if .Decide}}；等用户拍板{{end}}{{with .Parked}}；搁置中，等你定优先级：{{.}}{{end}}{{if .Overdue}}；已超过约定时间{{end}}{{range .Scope}}；范围调整：{{.}}{{end}}{{end}}{{end}}{{with .BriefsDone}}
- 上次简报以来结束的（只在这一次说，逐条标准的结果照写）：{{range .}}
  - {{if .Words}}{{.Date}} 你说：「{{.Words}}」{{else}}{{.Date}} {{if eq .By "routine"}}定时任务{{else if eq .By "hub"}}Persona{{else if eq .By "unknown"}}（没记下是谁要的）{{else}}助手{{end}}发起：{{.Goal}}{{end}} → {{if .Cancelled}}取消了{{else}}结束了{{end}}{{with .Now}}（{{.}}）{{end}}{{range .Result}}；「{{.Criterion}}」{{if eq .State "met"}}达成{{else if eq .State "not_met"}}没达成{{else if eq .State "dropped"}}已去掉{{with .Change}}（{{.}}）{{end}}{{else}}没核对{{end}}{{end}}{{range .AlsoDone}}；另外做了：{{.}}{{end}}{{end}}{{end}}{{with .ConfirmationSummary}}
- {{.}}{{range $.Confirmations}}
  - {{.}}{{end}}{{end}}{{with .Captures}}
- 随手记（Persona 算好的；带敏感类别的只给条数，不要猜它的内容、标题或归到了哪里{{if $.Card}}；飞书卡片里已经原样列出，你不用再列{{end}}）：{{range .}}
  - {{.}}{{end}}{{end}}{{with .Setup}}
- 建议的设置（Persona 按过去 7 天的记录算出来的：哪个还没开的机制本可以让用户少被问几次、或让对话少跑几轮后台任务，后面是代价和在哪里开；也可能有一行说后台督办用的模型可以换低一档，那一行按过去 30 天算，依据写在那一行里。{{if $.Card}}飞书卡片里已经原样列出，你不用再列。{{else}}请在回答的最后把这几行原样列出来，数字和措辞都不要改。{{end}}开不开、换不换由用户自己决定：你不要替用户打开或改动其中任何一项，也不要为此调用设置工具）：{{range .}}
  - {{.}}{{end}}{{end}}
