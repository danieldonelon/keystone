"""Build docs/Keystone-User-Manual.pdf."""

from pathlib import Path

from reportlab.lib.colors import HexColor, white
from reportlab.lib.pagesizes import letter
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import inch
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (
    Image,
    ListFlowable,
    ListItem,
    Paragraph,
    SimpleDocTemplate,
    Spacer,
    Table,
    TableStyle,
)

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs" / "Keystone-User-Manual.pdf"
LOGO = ROOT / "assets" / "keystone.png"

pdfmetrics.registerFont(TTFont("Segoe", r"C:\Windows\Fonts\segoeui.ttf"))
pdfmetrics.registerFont(TTFont("Segoe-Bold", r"C:\Windows\Fonts\segoeuib.ttf"))
pdfmetrics.registerFont(TTFont("Consolas", r"C:\Windows\Fonts\consola.ttf"))

BLACK = HexColor("#0D0D0D")
MINT = HexColor("#7BFFEC")
BONE = HexColor("#F4F7F6")
STEEL = HexColor("#5C6B70")
TEAL = HexColor("#0F6F68")
LINE = HexColor("#C5D0CB")
CONTENT = 7 * inch


class Manual(SimpleDocTemplate):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._outline_n = 0

    def afterFlowable(self, flowable):
        if not isinstance(flowable, Paragraph):
            return
        if flowable.style.name not in ("H1", "H2"):
            return
        text = flowable.getPlainText()
        self._outline_n += 1
        key = f"sec-{self._outline_n}"
        self.canv.bookmarkPage(key)
        level = 0 if flowable.style.name == "H1" else 1
        self.canv.addOutlineEntry(text, key, level=level, closed=False)


def styles():
    body = ParagraphStyle(
        "Body",
        fontName="Segoe",
        fontSize=10.5,
        leading=15,
        textColor=BLACK,
        spaceAfter=8,
    )
    return {
        "cover": ParagraphStyle("Cover", fontName="Segoe-Bold", fontSize=28, leading=32, textColor=BLACK, spaceAfter=4),
        "sub": ParagraphStyle("Sub", fontName="Segoe", fontSize=13, leading=18, textColor=STEEL, spaceAfter=12),
        "h1": ParagraphStyle("H1", fontName="Segoe-Bold", fontSize=15, leading=19, textColor=BLACK, spaceBefore=14, spaceAfter=6, keepWithNext=True),
        "h2": ParagraphStyle("H2", fontName="Segoe-Bold", fontSize=12, leading=16, textColor=BLACK, spaceBefore=10, spaceAfter=4, keepWithNext=True),
        "body": body,
        "bullet": ParagraphStyle("BulletBody", parent=body, leftIndent=0, spaceAfter=2),
        "code": ParagraphStyle("Code", fontName="Consolas", fontSize=8, leading=11, textColor=BLACK),
        "th": ParagraphStyle("TH", fontName="Segoe-Bold", fontSize=9, leading=12, textColor=BLACK),
        "td": ParagraphStyle("TD", fontName="Segoe", fontSize=9, leading=12, textColor=BLACK),
        "footer": ParagraphStyle("Footer", fontName="Segoe", fontSize=8, leading=10, textColor=STEEL),
    }


S = styles()


def P(text, style="body"):
    return Paragraph(text, S[style])


def code(text):
    safe = (
        text.replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
        .replace("\n", "<br/>")
    )
    block = Table([[Paragraph(safe, S["code"])]], colWidths=[CONTENT])
    block.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, -1), BONE),
        ("BOX", (0, 0), (-1, -1), 0.6, LINE),
        ("LEFTPADDING", (0, 0), (-1, -1), 8),
        ("RIGHTPADDING", (0, 0), (-1, -1), 8),
        ("TOPPADDING", (0, 0), (-1, -1), 6),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 6),
        ("VALIGN", (0, 0), (-1, -1), "TOP"),
    ]))
    return block


def bullets(items):
    flow = []
    for item in items:
        flow.append(ListItem(Paragraph(item, S["bullet"]), leftIndent=12, bulletColor=BLACK))
    return ListFlowable(flow, bulletType="bullet", start="•", leftIndent=16, bulletFontName="Segoe", bulletFontSize=10, spaceBefore=2, spaceAfter=8)


def table(headers, rows, widths):
    head = [Paragraph(h, S["th"]) for h in headers]
    body = [[Paragraph(cell, S["td"]) for cell in row] for row in rows]
    grid = Table([head] + body, colWidths=widths, repeatRows=1)
    grid.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, 0), BONE),
        ("TEXTCOLOR", (0, 0), (-1, -1), BLACK),
        ("FONTNAME", (0, 0), (-1, 0), "Segoe-Bold"),
        ("GRID", (0, 0), (-1, -1), 0.4, LINE),
        ("VALIGN", (0, 0), (-1, -1), "TOP"),
        ("LEFTPADDING", (0, 0), (-1, -1), 6),
        ("RIGHTPADDING", (0, 0), (-1, -1), 6),
        ("TOPPADDING", (0, 0), (-1, -1), 4),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 4),
        ("BACKGROUND", (0, 1), (-1, -1), white),
    ]))
    return grid


def header_footer(canvas, doc):
    canvas.saveState()
    width, height = letter
    canvas.setFillColor(BLACK)
    canvas.rect(0, height - 32, width, 32, fill=1, stroke=0)
    canvas.setFillColor(MINT)
    canvas.rect(0, height - 36, width, 4, fill=1, stroke=0)
    canvas.setFillColor(BONE)
    canvas.setFont("Segoe-Bold", 10)
    canvas.drawString(54, height - 21, "KEYSTONE")
    canvas.setFont("Segoe", 9)
    canvas.drawRightString(width - 54, height - 21, "User manual")
    canvas.setStrokeColor(LINE)
    canvas.setLineWidth(0.6)
    canvas.line(54, 42, width - 54, 42)
    canvas.setFillColor(STEEL)
    canvas.setFont("Segoe", 8)
    canvas.drawString(54, 28, "Version 0.1.1")
    canvas.drawRightString(width - 54, 28, str(doc.page))
    canvas.restoreState()


def story():
    logo = Image(str(LOGO), width=64, height=64)
    logo.hAlign = "LEFT"
    parts = [
        logo,
        Spacer(1, 10),
        P("Keystone", "cover"),
        P("User manual", "sub"),
        P("Keystone connects computers you own: this Windows laptop and Linux machines such as a Raspberry Pi. You get a private network, file transfer, and a screen viewer. ExpressVPN can stay connected while you use it."),
        P("This manual is everything you need to install Keystone, open it from the desktop, add a Pi, move files, view a screen, and reach your machines from another network."),
        Spacer(1, 6),
        P("What Keystone is", "h1"),
        P("Keystone builds its own network. The laptop that you set up first is the coordinator. Every other computer joins that coordinator with a token and a certificate pin. After that, the computers can see each other even when a commercial VPN owns the normal internet route."),
        P("Traffic between your Keystone computers uses WireGuard. Only those computers are on the network. Keystone does not send the rest of your browsing through a second tunnel, and it does not sign you into anyone else’s network."),
        P("The addresses inside the network are 10.91.0.0/16. The coordinator is 10.91.0.1. Later computers receive the next free address."),
        P("What you need", "h1"),
        bullets([
            "The Windows laptop, with the Keystone program in Projects\\Keystone\\dist\\keystone.exe.",
            "A Raspberry Pi on 64-bit or 32-bit Raspberry Pi OS. A 64-bit Pi uses keystone-linux-arm64. A 32-bit Pi uses keystone-linux-armv7.",
            "Both machines on the same home network for the first connection. Remote use is covered later and needs three ports forwarded on the home router.",
            "For a Pi screen, the package wayvnc or x11vnc. The laptop shares its own screen without an extra program.",
        ]),
        P("Set up the laptop", "h1"),
        P("Do this once, on the computer that should stay available to the others."),
        code(
            "cd C:\\Users\\danie\\Projects\\Keystone\n"
            ".\\dist\\keystone.exe init --name laptop\n"
            ".\\dist\\keystone.exe up"
        ),
        P("init creates the profile, the join token, and the certificate pin. up starts Keystone and opens http://127.0.0.1:8731. Leave this program running. It is the coordinator."),
        P("Windows asks the first time whether Keystone may listen. Allow it on private networks. If you choose Public, other computers on your home network may be blocked."),
        P("The shared folder starts as your home directory. You can change it later in Files."),
        P("The desktop icon", "h2"),
        P("The Keystone icon on the Windows desktop runs scripts\\launch-windows.ps1. If Keystone is already running, the icon opens the window. If it is stopped, the icon starts it and then opens the window. You do not get a command window."),
        P("To put the icon back after a new Windows user or a new PC:"),
        code("powershell -ExecutionPolicy Bypass -File scripts\\install-windows-shortcut.ps1"),
        P("The Keystone window", "h1"),
        P("The window is a page on this computer only. Another website cannot open it. The address is always http://127.0.0.1:8731."),
        bullets([
            "Machines lists this computer and every computer that has joined. A green dot means Keystone has heard from it recently.",
            "Files opens the shared folder on the computer you pick. You can open folders, download, upload, make a folder, and delete a file or an empty folder.",
            "Screen shows that computer’s desktop and sends your mouse and keyboard there.",
            "Light mode and Dark mode are at the right of the header. The choice is remembered in this browser.",
        ]),
        P("The mark is black with a MojoSoMint mint outline, #0D0D0D and #7BFFEC, the same colors as the desktop icon."),
        P("On the Machines page, Add a computer shows the exact join command for the next machine. Copy it. It contains the coordinator address, the token, and the pin. Treat that command like a password."),
        P("Add a Raspberry Pi", "h1"),
        P("On the Pi, download the program that matches the operating system, plus the installer. The commands below are for a 64-bit Pi. Replace the file name with keystone-linux-armv7 on a 32-bit Pi."),
        code(
            "curl -fL -o keystone-linux-arm64 \\\n"
            "  https://github.com/danieldonelon/keystone/releases/download/v0.1.1/keystone-linux-arm64\n"
            "curl -fL -o keystone.png \\\n"
            "  https://github.com/danieldonelon/keystone/releases/download/v0.1.1/keystone.png\n"
            "curl -fL -o install-pi.sh \\\n"
            "  https://raw.githubusercontent.com/danieldonelon/keystone/v0.1.1/scripts/install-pi.sh\n"
            "chmod +x keystone-linux-arm64"
        ),
        P("Join before you install the service, using the command from the laptop’s Keystone window. Change the name and the shared folder if you want something other than pi and /home/pi."),
        code(
            "sudo KEYSTONE_HOME=/var/lib/keystone ./keystone-linux-arm64 join \\\n"
            "  --name pi \\\n"
            "  --coordinator https://192.168.0.130:7707 \\\n"
            "  --token TOKEN \\\n"
            "  --pin PIN \\\n"
            "  --share /home/pi\n"
            "sudo sh install-pi.sh ./keystone-linux-arm64"
        ),
        P("install-pi.sh copies the program to /usr/local/bin/keystone, starts it at boot, and places a Keystone icon on the desktop and in the application menu. The icon opens http://127.0.0.1:8731 on the Pi."),
        P("If the desktop shows the icon as a text file, right-click it and choose Execute, or run chmod 755 on the desktop file. The installer already tries to mark it trusted."),
        P("The Pi must be able to reach the laptop on the coordinator address printed in the join command. On the same home Wi-Fi that address is the laptop’s LAN address. The laptop’s Keystone program has to be running."),
        P("Files", "h1"),
        P("Pick a computer, then Files. Click a folder to open it. Click a file to download it. Upload sends the files you choose into the folder you are viewing. New folder and Delete do what they say. Delete removes a file, or a folder only when the folder is empty. The shared folder itself cannot be deleted from the window."),
        P("Change, under the folder path, picks a different shared folder on that computer. The change is stored in that computer’s profile. A path outside the shared folder is refused."),
        P("Screen", "h1"),
        P("Pick a computer, then Screen, then Connect. On the laptop, Keystone captures the desktop itself and you can move the pointer and type. On a Pi, Keystone connects to a VNC server on that Pi."),
        code("sudo apt install wayvnc"),
        P("wayvnc is the usual choice on current Raspberry Pi OS. x11vnc works when the Pi is using an older X11 desktop. If the Pi already has a VNC server on 127.0.0.1 port 5900, Keystone uses that server. If the desktop asks for a VNC password, type it in the password box and connect again."),
        P("The screen view is the remote desktop, so clicks and keys go to that computer while the picture has focus."),
        P("ExpressVPN", "h1"),
        P("ExpressVPN can stay on. It installs broad routes so ordinary programs leave through the VPN. Keystone does not remove those routes and does not send your browser around the VPN."),
        P("Keystone pins only its own tunnel socket to the physical adapter, such as Wi-Fi. On Linux it marks those packets and sends the marked packets through the physical adapter. The Keystone window says which adapter it is using."),
        P("Check the split with:"),
        code("keystone doctor"),
        P("You want two different public addresses: one for an ordinary socket, and one for the socket Keystone pins. If both addresses are the same, ExpressVPN is still capturing Keystone. In the ExpressVPN app, add keystone.exe to split tunneling so that program bypasses the VPN. Then run doctor again."),
        P("Away from home", "h1"),
        P("On the same home network, Keystone uses the LAN and no router changes are required."),
        P("To reach the home Pi while the laptop is somewhere else, forward these ports on the home router to the laptop, because the laptop is the coordinator:"),
        table(
            ["Port", "Protocol", "Purpose"],
            [
                ["7707", "TCP", "Computers register and receive the member list."],
                ["7708", "UDP", "Discovery, and the relay when a direct tunnel cannot form."],
                ["51830", "UDP", "The direct WireGuard tunnel."],
            ],
            [0.85 * inch, 1.05 * inch, 5.1 * inch],
        ),
        Spacer(1, 8),
        P("If the direct tunnel does not come up, Keystone relays through the coordinator. The coordinator has to be running and reachable. A reservation for the laptop’s LAN address keeps the forward from breaking when the laptop gets a new DHCP address."),
        P("Desktop icons", "h1"),
        P("Windows", "h2"),
        P("Double-click Keystone on the desktop. The icon is a black keystone with a mint outline. The shortcut stays on the desktop; the program it starts is dist\\keystone.exe."),
        P("Raspberry Pi", "h2"),
        P("After install-pi.sh, Keystone is on the desktop and in the menu. It opens the window on the Pi. The background service keeps the network up even when the window is closed, including after a reboot."),
        P("Where information is stored", "h1"),
        bullets([
            "Windows profile: %APPDATA%\\Keystone. This holds the private key, the token, the pin, and the shared-folder setting.",
            "Pi service profile: /var/lib/keystone, because the service runs as root.",
            "A normal Linux login that is not the service uses ~/.config/keystone.",
        ]),
        P("Copying the profile copies the ability to join the network. Do not put that folder in a public place. The GitHub repository does not contain your token or your keys."),
        P("Commands", "h1"),
        table(
            ["Command", "What it does"],
            [
                ["keystone init", "Create the coordinator on the first computer."],
                ["keystone join", "Add this computer to a coordinator that is already running."],
                ["keystone up", "Start Keystone and open the window. --no-browser skips the window."],
                ["keystone status", "Show whether Keystone is running."],
                ["keystone doctor", "Check that the tunnel can bypass ExpressVPN."],
                ["keystone version", "Print the version. This manual matches 0.1.1."],
            ],
            [1.6 * inch, 5.4 * inch],
        ),
        Spacer(1, 8),
        P("Ports", "h1"),
        table(
            ["Port", "Who can use it", "What it is"],
            [
                ["8731", "This computer only", "The Keystone window."],
                ["7707", "Your computers", "Coordinator, protected by the certificate pin."],
                ["7708", "Your computers", "Discovery and relay."],
                ["51830", "Your computers", "WireGuard."],
                ["7760", "Inside the mesh", "File transfer."],
                ["5900", "Inside the mesh", "Screen."],
            ],
            [0.75 * inch, 1.55 * inch, 4.7 * inch],
        ),
        Spacer(1, 8),
        P("When something fails", "h1"),
        P("The icon does nothing, or a second start says the port is in use.", "h2"),
        P("Keystone is already running. Use the icon again; it should open the existing window. Or open http://127.0.0.1:8731 yourself. keystone status prints the same address when the program is up."),
        P("The Pi cannot join.", "h2"),
        P("The laptop must be running Keystone. The coordinator address in the join command must be an address the Pi can reach, usually the laptop’s Wi-Fi address. The token and pin must be copied completely. A firewall prompt that was denied on the laptop blocks the Pi. Allow Keystone on private networks and try the join again."),
        P("Both machines are online and files still fail.", "h2"),
        P("Run keystone doctor on the laptop. If the two public addresses match, exclude keystone.exe from ExpressVPN. On the Pi, the service must be active: systemctl status keystone."),
        P("The Pi screen is black or Connect fails.", "h2"),
        P("Install wayvnc, and be logged into the Pi desktop. Keystone can only show a desktop that is already running. If wayvnc asks for a password, enter it in the Screen page."),
        P("A computer disappeared from the list.", "h2"),
        P("The dot goes dark after the computer stops sending updates. Check that its Keystone service or program is running, and that it can still reach the coordinator."),
        P("Source and license", "h1"),
        P("The program source is at https://github.com/danieldonelon/keystone. Release v0.1.1 has the Windows program, both Pi programs, the icon, and this manual. Keystone’s own code is MIT. The WireGuard library it uses is MIT as well."),
    ]
    return parts


def main():
    OUT.parent.mkdir(exist_ok=True)
    doc = Manual(
        str(OUT),
        pagesize=letter,
        leftMargin=54,
        rightMargin=54,
        topMargin=56,
        bottomMargin=56,
        title="Keystone User Manual",
        author="Daniel Donelon",
        subject="How to install and use Keystone on Windows and Raspberry Pi",
    )
    doc.build(story(), onFirstPage=header_footer, onLaterPages=header_footer)
    print(OUT)


if __name__ == "__main__":
    main()
