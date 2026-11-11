import QtQuick
import QtCore
import QtQuick.Controls
import QtQuick.Controls as Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import Quickshell
import Quickshell.Io
import qs.Ui
import qs.Ui as Ui
import qs.Commons
import qs.Commons as Commons

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
  property string itemGroup: ""
  property string errorText: ""
  property string approvalError: ""
  property var recent: []
  property string recentQuery: ""
  readonly property var filteredRecent: recent.filter(path => path.toLowerCase().includes(recentQuery.toLowerCase().trim()))
  property var items: []
  property var fields: []
  property var requests: []
  property string requestId: ""
  property var review: ({})
  property string previousScreen: "recent"
  property string revealedField: ""
  property string revealedValue: ""
  property string revealPending: ""
  property bool focusSearchOnResults: false
  Commons.Translations { id: strings; directory: Qt.resolvedUrl("i18n") }

  component BodyText: Text {
    textFormat: Text.PlainText
    color: Commons.Color.foreground
    font.family: Style.font.family
    font.pixelSize: Style.font.body
  }
  component WideButton: Ui.Button {
    id: wideButton
    property string navigationList: ""
    property int navigationIndex: -1
    Layout.fillWidth: true
    implicitHeight: Style.space(42)
    bordered: true
    focusable: true
    leftAlign: true
    Keys.onUpPressed: function(event) {
      if (navigationIndex < 0) { event.accepted = false; return }
      root.moveList(navigationList, navigationIndex, -1)
      event.accepted = true
    }
    Keys.onDownPressed: function(event) {
      if (navigationIndex < 0) { event.accepted = false; return }
      root.moveList(navigationList, navigationIndex, 1)
      event.accepted = true
    }
  }

  onOpenedChanged: {
    if (opened) {
      Qt.callLater(function() { if (root.opened && root.screen === "recent") panel.loadedContent.recentFilter.forceActiveFocus() })
    } else {
      if (panel.loadedContent) panel.loadedContent.passwordField.text = ""
      fields = []
      clearReveal()
      if (screen === "fields") screen = "items"
    }
  }
  onScreenChanged: {
    if (screen !== "fields") clearReveal()
    Qt.callLater(function() {
      if (!root.opened || !panel.loadedContent) return
      if (root.screen === "password") panel.loadedContent.passwordField.forceActiveFocus()
      else if (root.screen === "items") panel.loadedContent.queryField.forceActiveFocus()
      else if (root.screen === "recent") panel.loadedContent.recentFilter.forceActiveFocus()
    })
  }

  function clearReveal() { revealedField = ""; revealedValue = ""; revealPending = ""; revealTimer.stop() }
  function toggleReveal(field) {
    if (revealedField === field || revealPending === field) { clearReveal(); return }
    clearReveal()
    revealPending = field
    send({action: "reveal", id: itemId, field: field})
  }

  function send(value) {
    if (service.running) service.write(JSON.stringify(value) + "\n")
    else errorText = strings.t("service.unavailable")
  }
  function handle(line) {
    let result
    try { result = JSON.parse(line) } catch (e) { errorText = strings.t("service.invalid_response"); return }
    if (result.action === "approval-status") { approvalError = result.error || ""; return }
    if (result.action === "request") { requests = requests.concat([result.id]); return }
    if (result.action === "request-closed") {
      requests = requests.filter(id => id !== result.id)
      if (requestId === result.id) { requestId = ""; review = ({}); if (screen === "approval") screen = previousScreen }
      return
    }
    if (result.action === "show-request") { root.open(); root.reviewRequest(result.id); return }
    if (result.action === "reveal") {
      if (result.error) { clearReveal(); errorText = result.error; return }
      if (!opened || screen !== "fields" || result.id !== itemId || result.field !== revealPending) return
      revealedField = result.field
      revealedValue = result.value || ""
      revealPending = ""
      revealTimer.interval = Math.max(1, Math.min(10, result.remaining || 10)) * 1000
      revealTimer.restart()
      return
    }
    errorText = result.error || ""
    if (result.error) return
    if (result.action === "recent") {
      recent = result.recent || []

    }
    else if (result.action === "open") { screen = "items"; focusSearchOnResults = true; send({action: "items", query: ""}) }
    else if (result.action === "items") {
      items = result.items || []
      if (focusSearchOnResults) {
        focusSearchOnResults = false
        Qt.callLater(function() {
          if (root.opened && root.screen === "items" && panel.loadedContent)
            panel.loadedContent.queryField.forceActiveFocus()
        })
      }
    }
    else if (result.action === "fields") {
      fields = (result.fields || []).filter(field => field.Name !== "Title")
      itemGroup = fields.length > 0 && fields[0].Group ? fields[0].Group : ""
      screen = "fields"
    }
    else if (result.action === "review") review = result
    else if (result.action === "approve" || result.action === "deny") { requestId = ""; review = ({}); screen = previousScreen }
    else if (result.action === "lock" || result.action === "locked") { screen = "recent"; items = []; fields = []; send({action: "recent"}) }
  }
  function displayPath(path) {
    const home = decodeURIComponent(StandardPaths.writableLocation(StandardPaths.HomeLocation).toString().replace(/^file:\/\//, "")).replace(/\/$/, "")
    return home && (path === home || path.startsWith(home + "/")) ? "~" + path.slice(home.length) : path
  }
  function openVault(path) { vaultPath = path; errorText = ""; screen = "password" }
  function openItem(item) { itemId = item.ID; itemTitle = item.Title; send({action: "fields", id: item.ID}) }
  function escapeView() {
    if (screen === "fields") { fields = []; screen = "items"; return true }
    if (screen === "items") { close(); return true }
    if (screen === "password") {
      if (panel.loadedContent) panel.loadedContent.passwordField.text = ""
      errorText = ""
      screen = "recent"
      return true
    }
    return false
  }
  function moveList(list, index, direction) {
    if (!panel.loadedContent) return
    const content = panel.loadedContent
    const count = list === "recent" ? filteredRecent.length : list === "password" ? 3 : list === "fields" ? fields.length + 1 : items.length
    const next = index + direction
    if (list === "recent" && next < 0) { content.recentFilter.forceActiveFocus(); return }
    if (list === "items" && next < 0) { content.queryField.forceActiveFocus(); return }
    if (next < 0 || next >= count) return
    const target = list === "recent"
      ? content.recentRepeater.itemAt(next)
      : list === "password"
        ? [content.passwordBackButton, content.passwordField, content.unlockButton][next]
        : list === "fields"
          ? (next === 0 ? content.fieldsBackButton : content.fieldRepeater.itemAt(next - 1))
          : content.itemRepeater.itemAt(next)
    if (!target) return
    target.forceActiveFocus()
    content.ensureVisible(target)
  }
  function reviewRequest(id) { if (screen !== "approval") previousScreen = screen; requestId = id; review = ({}); screen = "approval"; send({action: "review", id: id}) }

  Timer { id: revealTimer; repeat: false; onTriggered: root.clearReveal() }

  Process {
    id: service
    command: ["bypass", "widget-service"]
    running: true
    stdinEnabled: true
    stdout: SplitParser { onRead: data => root.handle(data) }
    onStarted: root.send({action: "recent"})
    onExited: { root.screen = "recent"; root.items = []; root.fields = []; root.errorText = strings.t("service.stopped") }
  }
  FileDialog {
    id: picker
    title: strings.t("vault.open")
    nameFilters: [strings.t("vault.kee_pass_filter"), strings.t("vault.all_files_filter")]
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
    focusTarget: !panel.loadedContent ? null : root.screen === "password" ? panel.loadedContent.passwordField : root.screen === "items" ? panel.loadedContent.queryField : root.screen === "recent" ? panel.loadedContent.recentFilter : panel.loadedContent
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(panel.loadedContentHeight, Style.space(560))
    content: ScrollView {
      id: scroll
      property alias passwordField: password
      property alias unlockButton: unlockButton
      property alias passwordBackButton: passwordBackButton
      property alias fieldsBackButton: fieldsBackButton
      property alias fieldRepeater: fieldRows
      property alias queryField: searchField
      property alias recentRepeater: recentRows
      property alias itemRepeater: itemRows
      property alias recentFilter: recentFilter
      function ensureVisible(item) {
        const view = scroll.contentItem
        const y = item.mapToItem(contentColumn, 0, 0).y
        if (y < view.contentY) view.contentY = y
        else if (y + item.height > view.contentY + view.height)
          view.contentY = Math.min(Math.max(0, view.contentHeight - view.height), y + item.height - view.height)
      }
      implicitHeight: contentColumn.implicitHeight
      contentWidth: availableWidth
      clip: true
      Keys.onEscapePressed: function(event) { event.accepted = root.escapeView() }
      Keys.onUpPressed: function(event) {
        if (root.screen !== "fields") { event.accepted = false; return }
        const view = scroll.contentItem
        if (view.contentY <= 0) { event.accepted = false; return }
        view.contentY = Math.max(0, view.contentY - Style.space(42))
        event.accepted = true
      }
      Keys.onDownPressed: function(event) {
        if (root.screen !== "fields") { event.accepted = false; return }
        const view = scroll.contentItem
        if (view.contentY + view.height >= view.contentHeight) { event.accepted = false; return }
        view.contentY = Math.min(view.contentHeight - view.height, view.contentY + Style.space(42))
        event.accepted = true
      }
      ColumnLayout {
        id: contentColumn
        width: scroll.availableWidth
        spacing: Style.space(14)
        PanelHero {
          Layout.fillWidth: true
          title: "bypass"
          meta: strings.t("vault.subtitle")
          iconComponent: Component {
            BodyText { text: "󰌾"; font.pixelSize: Style.font.display }
          }
        }
        BodyText { visible: root.errorText !== ""; text: root.errorText; color: Commons.Color.urgent; wrapMode: Text.Wrap; Layout.fillWidth: true }
        BodyText { visible: root.approvalError !== ""; text: root.approvalError; color: Commons.Color.urgent; wrapMode: Text.Wrap; Layout.fillWidth: true }
        WideButton { visible: root.requests.length > 0 && root.screen !== "approval"; text: strings.t("approval.review"); onClicked: root.reviewRequest(root.requests[0]) }
        ColumnLayout {
          visible: root.screen === "recent"
          Layout.fillWidth: true
          spacing: Style.space(8)
          PanelSeparator { Layout.fillWidth: true }
          RowLayout {
            Layout.fillWidth: true
            spacing: Style.space(8)
            TextField {
              id: recentFilter
              placeholderText: strings.t("search")
              Layout.fillWidth: true
              Layout.minimumWidth: 0
              Layout.maximumHeight: implicitHeight
              onTextChanged: root.recentQuery = text
              Keys.onDownPressed: function(event) { root.moveList("recent", -1, 1); event.accepted = true }
              Keys.onUpPressed: function(event) { event.accepted = true }
              onAccepted: if (root.filteredRecent.length > 0) root.openVault(root.filteredRecent[0])
            }
            Ui.Button {
              text: strings.t("vault.open_button")
              iconText: "󰝰"
              bordered: true
              focusable: true
              Layout.minimumHeight: recentFilter.implicitHeight
              Layout.maximumHeight: recentFilter.implicitHeight
              onClicked: picker.open()
            }
          }
          PanelSectionHeader { text: strings.t("vault.recent"); Layout.fillWidth: true }
          Repeater {
            id: recentRows
            model: root.filteredRecent
            WideButton {
              id: recentButton
              required property string modelData
              required property int index
              navigationList: "recent"
              navigationIndex: index
              bordered: false
              implicitHeight: searchField.implicitHeight
              Layout.minimumHeight: searchField.implicitHeight
              Layout.maximumHeight: searchField.implicitHeight
              text: root.displayPath(modelData)
              iconText: "󰌾"
              onClicked: root.openVault(recentButton.modelData)
            }
          }
        }
        ColumnLayout {
          visible: root.screen === "password"
          Layout.fillWidth: true
          spacing: Style.space(8)
          PanelSeparator { Layout.fillWidth: true }
          WideButton {
            id: passwordBackButton
            text: strings.t("back")
            iconText: "󰁍"
            navigationList: "password"
            navigationIndex: 0
            implicitHeight: searchField.implicitHeight
            Layout.minimumHeight: searchField.implicitHeight
            Layout.maximumHeight: searchField.implicitHeight
            onClicked: { password.text = ""; root.screen = "recent" }
          }
          BodyText { text: root.displayPath(root.vaultPath); elide: Text.ElideMiddle; Layout.fillWidth: true }
          TextField {
            id: password
            placeholderText: strings.t("vault.password")
            password: true
            Layout.fillWidth: true
            Keys.onDownPressed: function(event) { root.moveList("password", 1, 1); event.accepted = true }
            Keys.onUpPressed: function(event) { root.moveList("password", 1, -1); event.accepted = true }
            Keys.onEscapePressed: function(event) { event.accepted = root.escapeView() }
            onAccepted: { root.send({action: "open", path: root.vaultPath, password: text}); text = "" }
          }
          WideButton { id: unlockButton; text: strings.t("vault.unlock"); navigationList: "password"; navigationIndex: 2; onClicked: { root.send({action: "open", path: root.vaultPath, password: password.text}); password.text = "" } }
        }
        ColumnLayout {
          visible: root.screen === "items" || root.screen === "fields"
          Layout.fillWidth: true
          spacing: Style.space(8)
          PanelSeparator { Layout.fillWidth: true }
          RowLayout {
            Layout.fillWidth: true
            spacing: Style.space(8)
            TextField {
              id: searchField
              visible: root.screen === "items"
              placeholderText: strings.t("search")
              Layout.fillWidth: true
              Layout.minimumWidth: 0
              Layout.maximumHeight: implicitHeight
              onTextChanged: root.send({action: "items", query: text})
              Keys.onDownPressed: function(event) { root.moveList("items", -1, 1); event.accepted = true }
              Keys.onUpPressed: function(event) { root.moveList("items", -1, -1); event.accepted = true }
              Keys.onEscapePressed: function(event) { event.accepted = root.escapeView() }
              onAccepted: if (root.items.length > 0) root.openItem(root.items[0])
            }
            WideButton {
              id: fieldsBackButton
              visible: root.screen === "fields"
              text: strings.t("back")
              iconText: "󰁍"
              Layout.minimumWidth: 0
              implicitHeight: searchField.implicitHeight
              Layout.minimumHeight: searchField.implicitHeight
              Layout.maximumHeight: searchField.implicitHeight
              onClicked: { root.fields = []; root.screen = "items" }
            }
            Ui.Button {
              text: strings.t("vault.lock")
              iconText: "󰌾"
              bordered: true
              focusable: true
              Layout.minimumHeight: searchField.implicitHeight
              Layout.maximumHeight: searchField.implicitHeight
              onClicked: root.send({action: "lock"})
            }
          }
        }
        ColumnLayout {
          visible: root.screen === "items"
          Layout.fillWidth: true
          spacing: Style.space(8)
          PanelSectionHeader { text: searchField.text === "" ? strings.t("vault.favorites") : strings.t("vault.results"); Layout.fillWidth: true }
          Repeater {
            id: itemRows
            model: root.items
            WideButton {
              id: itemButton
              required property var modelData
              required property int index
              bordered: false
              implicitHeight: searchField.implicitHeight
              Layout.minimumHeight: searchField.implicitHeight
              Layout.maximumHeight: searchField.implicitHeight
              navigationList: "items"
              navigationIndex: index
              text: modelData.Title
              iconText: "󰌋"
              onClicked: root.openItem(itemButton.modelData)
            }
          }
        }
        ColumnLayout {
          visible: root.screen === "fields"
          Layout.fillWidth: true
          BodyText { text: root.itemTitle; font.pixelSize: Style.font.title; font.bold: true; elide: Text.ElideRight; Layout.fillWidth: true }
          Repeater {
            id: fieldRows
            model: root.fields
            ColumnLayout {
              id: fieldRow
              required property var modelData
              required property int index
              readonly property bool actionsHovered: fieldHover.hovered
              Layout.fillWidth: true
              HoverHandler { id: fieldHover }
              RowLayout {
                Layout.fillWidth: true
                BodyText { text: modelData.Name; color: Commons.Color.foreground; Layout.fillWidth: true }
                PanelActionButton { iconText: "󰌷"; tooltipText: "Copy bypass://" + (fieldRow.modelData.Group ? fieldRow.modelData.Group + "/" : "") + root.itemTitle + "#" + fieldRow.modelData.Name; focusable: true; opacity: fieldHover.hovered || activeFocus ? 1 : 0; onClicked: root.send({action: "copy-reference", id: root.itemId, field: modelData.Name}) }
              }
              RowLayout {
                Layout.fillWidth: true
                BodyText {
                  text: modelData.Protected ? (root.revealedField === modelData.Name ? root.revealedValue : "••••••••") : modelData.Value
                  color: Qt.darker(Commons.Color.foreground, 1.3)
                  wrapMode: modelData.Name === "Notes" ? Text.Wrap : Text.NoWrap
                  maximumLineCount: modelData.Name === "Notes" ? 5 : 1
                  elide: Text.ElideRight
                  Layout.fillWidth: true
                }
                PanelActionButton {
                  visible: modelData.Protected
                  iconText: root.revealedField === modelData.Name ? "󰈉" : "󰈈"
                  tooltipText: root.revealedField === modelData.Name ? strings.t("secret.hide") : strings.t("secret.reveal")
                  focusable: true
                  opacity: fieldRow.actionsHovered || activeFocus ? 1 : 0
                  onClicked: root.toggleReveal(modelData.Name)
                }
                PanelActionButton {
                  iconText: "󰆏"
                  tooltipText: strings.t("copy.value")
                  focusable: true
                  opacity: fieldRow.actionsHovered || activeFocus ? 1 : 0
                  onClicked: root.send({action: "copy", id: root.itemId, field: modelData.Name})
                }
              }
            }
          }
        }
        ColumnLayout {
          visible: root.screen === "approval"
          Layout.fillWidth: true
          PanelSeparator { Layout.fillWidth: true }
          WideButton { text: strings.t("back"); iconText: "󰁍"; onClicked: { root.review = ({}); root.screen = root.previousScreen } }
          PanelSectionHeader { text: strings.t("approval.title"); Layout.fillWidth: true }
          BodyText { text: strings.t("approval.agent").arg(root.review.agent || strings.t("approval.unknown")); Layout.fillWidth: true; wrapMode: Text.Wrap }
          BodyText { text: strings.t("approval.command").arg((root.review.command || []).join(" ")); Layout.fillWidth: true; wrapMode: Text.Wrap }
          BodyText { text: strings.t("approval.directory").arg(root.review.directory || ""); Layout.fillWidth: true; wrapMode: Text.Wrap }
          BodyText { text: strings.t("approval.identity_note"); wrapMode: Text.Wrap; Layout.fillWidth: true }
          Repeater {
            model: root.review.targets || []
            BodyText { required property var modelData; text: modelData.Group + "/" + modelData.Title + " #" + modelData.Field; Layout.fillWidth: true; wrapMode: Text.Wrap }
          }
          WideButton { text: strings.t("approval.approve"); enabled: (root.review.targets || []).length > 0; onClicked: root.send({action: "approve", id: root.requestId}) }
          WideButton { text: strings.t("approval.deny"); onClicked: root.send({action: "deny", id: root.requestId}) }
        }
      }
    }
  }
}
