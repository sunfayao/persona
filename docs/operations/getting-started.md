# Getting started with Persona

This page is for people who don't write code and don't usually open a terminal. Follow it and you'll have Persona installed, your browser signed in to it, a computer and a model hooked up, and your first message sent.
Where a step still needs the terminal, and what isn't possible yet, is spelled out below.

If you're comfortable with the command line and want to build and deploy it yourself, see [README](../../README.md#get-started) ("Get started").

## Persona has three parts

- **hub**: the home switchboard. It runs on a computer or NAS that stays on. Your messages, settings and memory are stored here, and only on your own machine. One per household is enough.
- **Your computers**: where Persona does the work: reading files, running programs, writing code. If Claude Code was installed on the hub machine before you installed the hub, that machine counts as one automatically; if you add Claude Code only after installing the hub, it doesn't (see "Connect a computer").
- **Web app**: the remote control, in any browser. See what it's doing and click **Allow** before it acts. Sign in once with a pairing link; it stays signed in after that.

You also need a **model**, Persona's brain: if you already use Claude Code, use that; if you have an API key, paste it in; if you have neither, the wizard tells you where to sign up.

```
   Web app ──sign in once──▶  hub (the always-on one)  ◀──Persona Link── your laptop
   (remote control)           stores, receives, schedules      (where work happens)
                                    │
                               also counts as a computer itself
                                    │
                              model (Claude Code or API key)
```

### Who runs what

No central backend holds your data. Everything above is yours: you run the hub, on your own always-on Linux machine, Mac or NAS, and your computers do the work. Your messages, settings and memory stay on the hub ([the table below](#first-check-what-you-have) says which machine suits you).

The hub is in the public repository (`server/internal/hub`); what a deployment holds, such as its signing keys, is the operator's own configuration and is not in it.

## First, check what you have

| What you have | Where the hub goes | How far you can get now |
| --- | --- | --- |
| An always-on Linux computer (mini PC, old desktop) + a browser | That Linux machine | The smoothest path: paste one command to install the hub, sign in from a browser, connect a model. Install Claude Code on that machine before the hub, and you don't need another computer |
| Only a Mac | This Mac | Install Docker Desktop or OrbStack first (the hub's database runs on it), then paste one command. When you close the lid, Persona rests too |
| A Mac laptop + an always-on Linux | Linux | Install the hub on Linux and connect the Mac as "your computer". Persona Link on the Mac currently has to be built yourself |
| A NAS (Synology, QNAP, etc.) | The NAS's Docker | The hub installs, but no work can happen on the NAS, so you must add another computer. The install package currently needs a GitHub login to download; suited to people who know Docker, see [docker.md](docker.md) |
| A work computer (can't install software) | A Linux at home, a NAS or a cloud server | The hub installs and the browser connects; the main chat still needs a computer that can run Claude Code |
| A Windows computer | Can't be the hub | Can only be "your computer", and currently has to be built yourself |

If you have an always-on computer, put the hub on it. A laptop works as the hub too, but when the lid is closed and it sleeps, your browser can't connect.

## Open Persona

Persona is a web app: once the hub is set up, open the hub's address in a browser (explained below). On a computer you can also install it as an app ([web.md](../web/web.md), "On computers"). No local node is required just to chat or use Agents on another computer. A computer whose Agents Persona should reach also needs the separately installed Persona Link ("Connect a computer" below).

## Install the hub: paste one command

This step uses the terminal, and you do it once, only on the hub machine. If you want the hub machine to do work too, install Claude Code on it before installing the hub: the hub installer checks whether this machine has Claude Code, counts it as a computer only if it does, and never checks again.
The web side never needs the terminal; installing Claude Code on a computer and connecting a Linux computer will need it, and even then it's just pasting one line.

1. Open **Terminal** on the computer that will be the hub. Mac: press Command + Space, type "Terminal", press Return. Linux: find "Terminal" in the app list.
2. Copy this command, paste it into the terminal and press Return:

   ```sh
   curl -fsSL https://github.com/nwverse/persona/releases/latest/download/install.sh | sh -s -- --hub
   ```

   It installs the newest GitHub Release, and the hub it sets up updates itself from GitHub Releases. The Persona website
   ([personaside.com](https://personaside.com/#start); in Chinese for now) shows the same command.

3. It installs step by step: download and verify the program, set up the database, make it start at boot, run a check. It prints one line per finished step.
   Partway through it may ask once whether to use administrator rights, so the hub can run even when nobody is logged in; answer "yes" and type this computer's login password.
4. At the end the screen shows:
   - a pairing link (and its QR code) to sign in from a browser in the next section, valid for 10 minutes;
   - the admin page address, where you can set an admin password. You can do this later.

When a step says something is wrong, the "Next step" line below it says what to do. The most common one is "this computer is missing a database": on a Mac install Docker Desktop or OrbStack, on Linux install Docker, then paste the same command again.
Steps already done aren't redone, so rerunning is safe; if the QR code expires, get a new one as in "Pair again" below.

## Sign in from a browser

1. Open the hub's address in a browser. That's the part before `/admin` in the admin page address the hub installer printed, like `http://<LAN address>:18660`.
2. On the sign-in page, paste the pairing link the installer printed into the **Paste a sign-in or pairing link** box and submit it with the arrow. You can also sign in with the hub's master key under **Advanced**; it's the `token` in `hub.json`, and whoever has it can do anything.

If it doesn't connect:

- The browser must be able to reach the hub's address (the same Wi-Fi at home).
- The pairing link is valid for 10 minutes and works once. If it expired, get a new one as in "Pair again" below.

For now it's only guaranteed to work on the same network at home. To connect back home while out, the only no-terminal option is to set up a VPN yourself. For direct IPv6, besides turning on IPv6 on your router, you also need to run one more command in the hub machine's terminal (see "Advanced", [network.md](network.md)).

### Another browser

A browser that's already signed in can let another in, without going back to the hub machine: open **Settings › Devices › Connect New Device**, choose **Browser**, and open the sign-in link it shows (or its QR code) in the other browser.

## Pair again

A pairing link is valid for 10 minutes and works once. To get a new one, open **Terminal** on the hub machine and run:

```sh
personad hub pair
```

It prints a new pairing link (and its QR code), just like the end of the install. If the terminal answers `command not found`, run `~/.persona/bin/personad hub pair` instead. Pasting the install command again works too: it redoes nothing that's already done and ends with a new code. For a hub in Docker on a NAS, see [docker.md](docker.md).

| What happened | What to do |
| --- | --- |
| The link expired before you used it | Get a new link and paste it |
| A browser was signed out | Sign it in again from another signed-in browser (**Settings › Devices › Connect New Device**), or get a new link on the hub machine |
| The hub was installed again, or moved to another machine | Get a new link on the machine the hub runs on now and sign in again |
| A computer was removed from the machine list | Add it again on the web as in "Connect a computer" below: **Machines › Add computer** |

## Connect a computer

If Claude Code was installed on the hub machine before the hub, that machine is already your first computer and you can skip this section.
If you added Claude Code to it only after installing the hub, run the hub install command (`personad hub setup`) once more in a terminal and it adds that machine as your first computer; you can also add it by hand (see "Advanced").

To add another computer (say, your Mac laptop), do it on the web:

1. Open **Machines** in the web version and click **Add computer**.
2. Choose that computer's system and give it a name, for example "My MacBook".
3. Follow the on-screen instructions on that computer; each system is different:
   - **Linux**: copy the line on screen, open a terminal on that computer, paste, press Return. It installs Persona's background service and connects by itself.
   - **Mac**: you first need the independently installed Persona Link background service. Follow [persona-link-install.md](persona-link-install.md) step by step.
     Once installed, send the pairing link to that Mac (with **Copy Pairing Link** or **Share…**) and use the `persona-link pair` command shown there. Link starts at login and does not need a menu bar app.
   - **Windows**: build two programs yourself, then paste one line in PowerShell ([windows.md](windows.md)).
4. Once that computer connects, the same screen asks "A computer wants to join: <name>. Is it yours?". If it's yours click **It's mine**; if you don't recognize it click **No, remove**.

Why confirm again: if someone else gets hold of the pairing link, their computer can connect too. Until you confirm, the computer gets none of your model keys and Persona won't do work on it; if it isn't confirmed within 24 hours, it's removed automatically.
So only send the pairing link to your own computers; don't paste it in a group chat. The link is valid for 10 minutes and works once; if it expired, click **Regenerate** on the same screen.

Closing that screen is fine: the computer's card in the machine list shows an orange notice, and you can confirm or remove it there.

Use **Add Computer** on the web.

## Connect a model

The main chat always runs on a computer with Claude Code installed. Even if you use another vendor's API key, it's the Claude Code on that computer that connects to it. So first make sure your computer has Claude Code; if it doesn't, the card above the main chat shows you how to install it (see the next section). If you only use the hub machine and Claude Code wasn't there when you installed the hub, install it and run `personad hub setup` once more in a terminal (see "Install the hub").

Pick a path based on what you have:

- **Already using Claude Code and signed in**: nothing to fill in; just send something in the main chat.
- **Have an API key**: in the web version open **Settings › Model Providers › Add Provider**, choose a vendor from the list and paste the key. It sends one very short message as a test, which costs a tiny amount.
  Once saved, the main chat and newly opened Agents use it automatically; if you previously chose a different model in **Model Settings**, your choice is kept and not replaced.
  Grok is the exception: it can't run the main chat or Agents, only background work such as sorting and processing.
- **Have nothing**: pick a vendor from the same list, click **Where do I get a key?** to sign up on their site and copy a key, then come back and paste it.

The list currently has: Anthropic official, DeepSeek, Kimi (Moonshot AI), Zhipu GLM, Alibaba Cloud Model Studio (Qwen), MiniMax, and Grok (xAI); Kimi and MiniMax also have international editions.
The ones marked as directly available in mainland China can be opened with a Chinese phone number and payment method; Anthropic official, Grok and the two international editions need an overseas account and payment method. If none fit, choose **Custom compatible gateway** at the end of the list.

The web has the "pick a vendor, just paste the key" wizard.
## What the cards above the main chat mean

In the web version's main chat, when something isn't connected yet, a card appears above the input box saying which step is missing, with a button. The send button also changes to text like **Add a computer first** or **Connect a model first**; clicking it takes you straight to that step.
What you typed stays in the input box, to send once things are connected. When the computer is connected and the model is set up, the card disappears by itself, no refresh needed. No card means everything is connected; try sending "hello".

| The card says | Meaning | What to do |
| --- | --- | --- |
| Add your computers | There's no computer that can do work yet, or one was added but not confirmed | Add one following "Connect a computer" above; for one you already added, click **It's mine** in the machine list |
| <computer name> is offline | That computer is asleep, or its Persona Link isn't running | Open the lid, wake it up, make sure Persona Link is running |
| Persona on <computer name> is too old | The Persona program on that computer needs updating | Update Persona Link on that computer. On a Mac you built it yourself, so build it again |
| Claude Code isn't installed | That computer has no Claude Code | Click **How to install**: open a terminal on that computer, paste the line the card gives you, press Return |
| Claude Code isn't signed in | It's installed but not signed in | Click **How to sign in**: open a terminal on that computer, type `claude`, press Return, and sign in once in the browser as prompted. Or click **Use an API key** |
| The model's key is wrong | The key was mistyped or has expired | Click **Change the key** and paste one that works |
| This model isn't available now | The model name is wrong, or the vendor has paused that model | Pick another model in **Settings › Model Providers** |

If it fails only after sending, the card appears under that message, worded the same as in the table. Open **Details** to see the raw error; screenshot that when asking someone for help.

## What isn't possible yet, and what still needs the terminal

- Installing the hub means pasting one command in that computer's terminal, once.
- A new pairing link, when no other browser is still signed in, takes one command in the hub machine's terminal ("Pair again").
- A Mac as the hub needs Docker Desktop or OrbStack first.
- The Windows programs have no packages yet; you build them yourself.
- Installing Claude Code on a computer and signing in to Claude Code each have to be done once in that computer's terminal.
- With only an API key and no computer, the main chat doesn't work; you must connect a computer that has Claude Code.
- The NAS install package needs a GitHub login to download.
- Connecting back home while out needs a VPN you set up yourself; direct IPv6 also needs one command run in the hub machine's terminal.
- If Claude Code was installed on the hub machine only after the hub, rerun `personad hub setup` to add it as a computer, or add it by hand (see "Advanced").

## Advanced

For people comfortable with the command line. The commands and files behind each step above:

- The hub install command ends by running `personad hub setup`: it writes `~/.persona/hub.json`, creates the Postgres container `persona-postgres` in Docker, installs a systemd user service (on a Mac, the LaunchAgent `com.persona.hub`), runs a self-check, and finally prints the pairing QR code and `/admin#setup=<setup code>`.
  What it does and doesn't change is in [onboarding.md](../assistant/onboarding.md) ("Installer"); for manual deployment see [server.md](../server/server.md), for a NAS see [docker.md](docker.md).
- Get another pairing link: run `personad hub pair` on the hub machine ("Pair again" above). Under "Advanced" on the web sign-in page you can sign in with the hub's master key; it's the `token` in hub.json, and whoever has it can do anything.
- Add a computer without the web: **Admin › Computers › Add computer**, or run `personad hub pair --machine <id> --add --name <name>` on the hub machine. Computers added either way count as confirmed right away. Details in [link.md](link.md).
- If the machine had no `claude` or `herdr` when the hub was installed, `machines` in `hub.json` is empty. After adding Claude Code, rerunning `personad hub setup` adds this machine (`kind: local`) while the hub still has no computer (`machines` empty and none kept by a manager); or edit hub.json in the admin page (or directly), add an entry `{"id": "<hostname>", "name": "<name>", "kind": "local"}` to `machines`, and restart the hub ([config.md](config.md)).
- A freshly installed hub only opens its TLS port on LAN IPv4; to connect directly from outside too, rerun `personad hub setup --outside`, see [network.md](network.md).
- If you already run claude through aliases or functions in `~/.zshrc` or `~/.bashrc`, they're still discovered and selectable, and **Import** beside **Add provider** in **Settings › Model Providers** turns a computer's aliases into providers, keeping their settings; see [launchers.md](../integrations/launchers.md).
- The APIs for main chat readiness and error codes are in [protocol.md](../protocol/protocol.md) ("Main chat readiness"); design and future plans are in [onboarding.md](../assistant/onboarding.md).
