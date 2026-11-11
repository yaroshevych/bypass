import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import Quickshell
import Quickshell.Io
import qs.Ui
import qs.Commons

Panel {
  id: root
  moduleName: "bypass.vault"
  ipcTarget: "bypass.vault"
  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight
  property string screen: "recent"
  property string vaultPath: ""
  property string itemId: ""
  property string itemTitle: ""
  property string errorText: ""
  property string approvalError: ""
  property var recent: []
  property var items: []
  property var fields: []
  property var requests: []
  property string requestId: ""
  property var review: ({})
  property string previousScreen: "recent"

  onOpenedChanged: {
    if (!opened) {
      password.text = ""
      fields = []
      if (screen === "fields") screen = "items"
    }
  }

  function send(value) {
    if (service.running) service.write(JSON.stringify(value) + "\n")
    else errorText = "Bypass service is unavailable"
  }
  function handle(line) {
    let result
    try { result = JSON.parse(line) } catch (e) { errorText = "Invalid response from Bypass"; return }
    if (result.action === "approval-status") { approvalError = result.error || ""; return }
    if (result.action === "request") { requests = requests.concat([result.id]); return }
    if (result.action === "request-closed") {
      requests = requests.filter(id => id !== result.id)
      if (requestId === result.id) { requestId = ""; review = ({}); if (screen === "approval") screen = previousScreen }
      return
    }
    if (result.action === "show-request") { root.open(); root.reviewRequest(result.id); return }
    errorText = result.error || ""
    if (result.error) return
    if (result.action === "recent") recent = result.recent || []
    else if (result.action === "open") { screen = "items"; send({action: "items", query: ""}) }
    else if (result.action === "items") items = result.items || []
    else if (result.action === "fields") { fields = result.fields || []; screen = "fields" }
    else if (result.action === "review") review = result
    else if (result.action === "approve" || result.action === "deny") { requestId = ""; review = ({}); screen = previousScreen }
    else if (result.action === "lock" || result.action === "locked") { screen = "recent"; items = []; fields = []; send({action: "recent"}) }
  }
  function openVault(path) { vaultPath = path; errorText = ""; screen = "password" }
  function reviewRequest(id) { if (screen !== "approval") previousScreen = screen; requestId = id; review = ({}); screen = "approval"; send({action: "review", id: id}) }

  Process {
    id: service
    command: ["bypass", "widget-service"]
    running: true
    stdinEnabled: true
    stdout: SplitParser { onRead: data => root.handle(data) }
    onStarted: root.send({action: "recent"})
    onExited: { root.screen = "recent"; root.items = []; root.fields = []; root.errorText = "Bypass service stopped" }
  }
  FileDialog {
    id: picker
    title: "Open vault"
    nameFilters: ["KeePass vaults (*.kdbx)", "All files (*)"]
    onAccepted: root.openVault(decodeURIComponent(selectedFile.toString().replace(/^file:\/\//, "")))
  }
  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "󰌾"
    onPressed: root.toggle()
  }
  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: panel.loadedContent
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(Style.space(500))
    content: ScrollView {
      clip: true
      ColumnLayout {
        width: parent.width
        spacing: 8
        Label { text: "Bypass"; font.bold: true; font.pixelSize: 22 }
        Label { visible: root.errorText !== ""; text: root.errorText; textFormat: Text.PlainText; wrapMode: Text.Wrap; Layout.fillWidth: true }
        Label { visible: root.approvalError !== ""; text: root.approvalError; textFormat: Text.PlainText; wrapMode: Text.Wrap; Layout.fillWidth: true }
        Button { visible: root.requests.length > 0 && root.screen !== "approval"; text: "Review request"; onClicked: root.reviewRequest(root.requests[0]) }
        ColumnLayout {
          visible: root.screen === "recent"
          Layout.fillWidth: true
          Repeater {
            model: root.recent
            Button {
              id: recentButton
              required property string modelData
              text: modelData
              contentItem: Label { text: recentButton.text; textFormat: Text.PlainText; elide: Text.ElideMiddle }
              Layout.fillWidth: true
              onClicked: root.openVault(modelData)
            }
          }
          Button { text: "Open vault…"; Layout.fillWidth: true; onClicked: picker.open() }
        }
        ColumnLayout {
          visible: root.screen === "password"
          Layout.fillWidth: true
          Label { text: root.vaultPath; textFormat: Text.PlainText; elide: Text.ElideMiddle; Layout.fillWidth: true }
          TextField {
            id: password
            placeholderText: "Password"
            echoMode: TextInput.Password
            Layout.fillWidth: true
            onAccepted: { root.send({action: "open", path: root.vaultPath, password: text}); text = "" }
          }
          Button { text: "Unlock"; Layout.fillWidth: true; onClicked: { root.send({action: "open", path: root.vaultPath, password: password.text}); password.text = "" } }
          Button { text: "Back"; onClicked: { password.text = ""; root.screen = "recent" } }
        }
        ColumnLayout {
          visible: root.screen === "items"
          Layout.fillWidth: true
          TextField { placeholderText: "Search"; Layout.fillWidth: true; onTextChanged: root.send({action: "items", query: text}) }
          Repeater {
            model: root.items
            Button {
              id: itemButton
              required property var modelData
              text: modelData.Title
              contentItem: Label { text: itemButton.text; textFormat: Text.PlainText; elide: Text.ElideRight }
              Layout.fillWidth: true
              onClicked: { root.itemId = modelData.ID; root.itemTitle = modelData.Title; root.send({action: "fields", id: modelData.ID}) }
            }
          }
          Button { text: "Lock"; onClicked: root.send({action: "lock"}) }
        }
        ColumnLayout {
          visible: root.screen === "fields"
          Layout.fillWidth: true
          Button { text: "← Back"; onClicked: { root.fields = []; root.screen = "items" } }
          Label { text: root.itemTitle; textFormat: Text.PlainText; font.bold: true }
          Repeater {
            model: root.fields
            ColumnLayout {
              required property var modelData
              Layout.fillWidth: true
              RowLayout {
                Layout.fillWidth: true
                Label { text: modelData.Name; textFormat: Text.PlainText; Layout.fillWidth: true }
                Button { text: "Copy path"; onClicked: root.send({action: "copy-reference", id: root.itemId, field: modelData.Name}) }
              }
              RowLayout {
                Layout.fillWidth: true
                Label { text: modelData.Protected ? "••••••••" : modelData.Value; textFormat: Text.PlainText; elide: Text.ElideRight; Layout.fillWidth: true }
                Button { text: "Copy"; onClicked: root.send({action: "copy", id: root.itemId, field: modelData.Name}) }
              }
            }
          }
        }
        ColumnLayout {
          visible: root.screen === "approval"
          Layout.fillWidth: true
          Button { text: "← Back"; onClicked: { root.review = ({}); root.screen = root.previousScreen } }
          Label { text: "Secret access request"; font.bold: true }
          Label { text: "Agent: " + (root.review.agent || "Unknown"); textFormat: Text.PlainText; Layout.fillWidth: true; wrapMode: Text.Wrap }
          Label { text: "Command: " + (root.review.command || []).join(" "); textFormat: Text.PlainText; Layout.fillWidth: true; wrapMode: Text.Wrap }
          Label { text: "Directory: " + (root.review.directory || ""); textFormat: Text.PlainText; Layout.fillWidth: true; wrapMode: Text.Wrap }
          Label { text: "Agent identity is self-claimed and not verified."; wrapMode: Text.Wrap; Layout.fillWidth: true }
          Repeater {
            model: root.review.targets || []
            Label { required property var modelData; text: modelData.Group + "/" + modelData.Title + " #" + modelData.Field; textFormat: Text.PlainText; Layout.fillWidth: true; wrapMode: Text.Wrap }
          }
          Button { text: "Approve once"; enabled: (root.review.targets || []).length > 0; Layout.fillWidth: true; onClicked: root.send({action: "approve", id: root.requestId}) }
          Button { text: "Deny"; Layout.fillWidth: true; onClicked: root.send({action: "deny", id: root.requestId}) }
        }
      }
    }
  }
}
