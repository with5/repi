package io.github.with5.repi.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.with5.repi.DesktopMaterial
import io.github.with5.repi.Material
import io.github.with5.repi.SyncStatus
import io.github.with5.repi.syncStatus
import io.github.with5.repi.R
import io.github.with5.repi.RemoteModel
import io.github.with5.repi.RemoteState
import io.github.with5.repi.RemoteVoice
import io.github.with5.repi.isChinese

private val speeds = listOf(0.8f, 0.9f, 1f, 1.1f, 1.2f)
private const val enSample = "The quick brown fox jumps over the lazy dog. Can you hear the difference?"
private const val zhSample = "今天天气很好，我们一起去公园散步吧。"

/** Has the computer read a pasted text with one of its voices. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RemoteScreen(
    model: RemoteModel,
    onBack: () -> Unit,
    pausePlayer: () -> Unit,
    onReplaced: (String) -> Unit,
    onDone: (Material) -> Unit,
) {
    val state by model.state.collectAsStateWithLifecycle()
    var title by rememberSaveable { mutableStateOf("") }
    var text by rememberSaveable { mutableStateOf("") }
    var editing by rememberSaveable { mutableStateOf(false) }
    var pasteTab by rememberSaveable { mutableStateOf(false) }
    val phone by model.library.materials.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val zh = text.isNotBlank() && isChinese(text)
    val lang = if (zh) "zh" else "en"
    val busy = state.busy

    LaunchedEffect(Unit) { model.refresh() } // the desktop may have new material

    LaunchedEffect(state.error) {
        state.error?.let {
            snackbar.showSnackbar(it)
            model.errorShown()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("连接电脑") },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(painterResource(R.drawable.ic_back), "返回") }
                },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        LazyColumn(
            Modifier.fillMaxSize().imePadding(),
            contentPadding = PaddingValues(
                start = 16.dp, end = 16.dp,
                top = padding.calculateTopPadding() + 8.dp,
                bottom = padding.calculateBottomPadding() + 24.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Connection(model, state, editing || state.voices.isEmpty(), onEdit = { editing = it })
            }
            if (state.voices.isEmpty()) return@LazyColumn

            item {
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    SegmentedButton(!pasteTab, { pasteTab = false }, SegmentedButtonDefaults.itemShape(0, 2)) {
                        Text("电脑上的材料")
                    }
                    SegmentedButton(pasteTab, { pasteTab = true }, SegmentedButtonDefaults.itemShape(1, 2)) {
                        Text("粘贴文字")
                    }
                }
            }
            if (!pasteTab) {
                desktopMaterials(model, state, phone, onReplaced)
                return@LazyColumn
            }

            item {
                OutlinedTextField(
                    title, { title = it }, Modifier.fillMaxWidth(),
                    label = { Text("标题（可不填）") }, singleLine = true, enabled = !busy,
                )
            }
            item {
                OutlinedTextField(
                    text, { text = it }, Modifier.fillMaxWidth(),
                    label = { Text("要读的英文或中文") }, minLines = 6, maxLines = 14, enabled = !busy,
                    supportingText = {
                        if (text.isNotBlank()) Text("识别为${if (zh) "中文" else "英语"}，用下面选中的${if (zh) "中文" else "英语"}音色读")
                    },
                )
            }
            item {
                Text("语速", style = MaterialTheme.typography.labelLarge)
                Spacer(Modifier.size(4.dp))
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    speeds.forEachIndexed { i, v ->
                        SegmentedButton(
                            selected = state.speed == v,
                            onClick = { model.setSpeed(v) },
                            shape = SegmentedButtonDefaults.itemShape(i, speeds.size),
                            enabled = !busy,
                        ) { Text("${v}x") }
                    }
                }
            }
            if (!zh) {
                item {
                    Row(
                        Modifier.fillMaxWidth().clickable(enabled = !busy) { model.setTranslate(!state.translate) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(state.translate, { model.setTranslate(it) }, enabled = !busy)
                        Text("附上中文翻译（电脑联网翻译）")
                    }
                }
            }
            item { Progress(model, state, enabled = text.isNotBlank()) { model.make(title, text, onDone) } }

            val voices = state.voices.filter { it.lang == lang }
            val chosen = if (zh) state.zhVoice else state.voice
            item {
                Text(
                    "${if (zh) "中文" else "英语"}音色（${voices.size}）· 点 ▶ 试听",
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
            if (voices.isEmpty()) {
                item {
                    Text(
                        "电脑上还没有能读${if (zh) "中文" else "英语"}的音色，在电脑版 Repi 的音色里安装。",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            items(voices, key = { it.name }) { v ->
                val sample = text.takeIf { it.isNotBlank() } ?: if (zh) zhSample else enSample
                VoiceRow(
                    v, selected = v.name == chosen,
                    playing = state.auditioning == v.name, loading = state.auditioning == v.name && state.auditionLoading,
                    onSelect = { if (!busy) model.choose(v) },
                    onPlay = { pausePlayer(); model.audition(v, sample) },
                )
            }
        }
    }
}

@Composable
private fun Connection(model: RemoteModel, state: RemoteState, editing: Boolean, onEdit: (Boolean) -> Unit) {
    var url by rememberSaveable { mutableStateOf(model.url) }
    var token by rememberSaveable { mutableStateOf(model.token) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (!editing) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("已连接电脑", style = MaterialTheme.typography.titleSmall)
                        Text(
                            "${model.url} · ${state.voices.size} 个音色",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                    TextButton(onClick = { model.connect(model.url, model.token) }) { Text("刷新") }
                    TextButton(onClick = { onEdit(true) }) { Text("更改") }
                }
                return@Column
            }
            Text(
                "在电脑上运行 repi serve，把它显示的地址和令牌填在这里。手机和电脑要在同一个网络。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                url, { url = it }, Modifier.fillMaxWidth(),
                label = { Text("地址") }, placeholder = { Text("192.168.1.5:7849") }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
            OutlinedTextField(
                token, { token = it }, Modifier.fillMaxWidth(),
                label = { Text("令牌") }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii),
            )
            state.connectError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Button(
                    onClick = { model.connect(url, token); onEdit(false) },
                    enabled = url.isNotBlank() && token.isNotBlank() && !state.connecting,
                ) { Text("连接") }
                if (state.connecting) {
                    Spacer(Modifier.width(12.dp))
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                }
                if (state.voices.isNotEmpty()) {
                    Spacer(Modifier.weight(1f))
                    TextButton(onClick = { onEdit(false) }) { Text("取消") }
                }
            }
        }
    }
}

@Composable
private fun Progress(model: RemoteModel, state: RemoteState, enabled: Boolean, onMake: () -> Unit) {
    val job = state.job
    when {
        state.downloading -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("正在传到手机…")
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        job != null -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    if (job.done == 0) "电脑正在准备…" else "电脑合成中 ${job.done}/${job.total}",
                    Modifier.weight(1f),
                )
                OutlinedButton(onClick = model::cancel) { Text("取消") }
            }
            if (job.total > 0 && job.done > 0) {
                LinearProgressIndicator({ job.done.toFloat() / job.total }, Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        }
        else -> Button(onClick = onMake, enabled = enabled, modifier = Modifier.fillMaxWidth()) {
            Text("让电脑合成，并加入材料")
        }
    }
}

@Composable
private fun VoiceRow(
    v: RemoteVoice,
    selected: Boolean,
    playing: Boolean,
    loading: Boolean,
    onSelect: () -> Unit,
    onPlay: () -> Unit,
) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onSelect), verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected, onSelect)
        Column(Modifier.weight(1f)) {
            Text(v.label)
            if (v.detail.isNotBlank()) {
                Text(v.detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        IconButton(onClick = onPlay) {
            when {
                loading -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                playing -> Icon(painterResource(R.drawable.ic_stop), "停止")
                else -> Icon(painterResource(R.drawable.ic_play), "试听")
            }
        }
    }
}

/** The computer's materials, each with whether the phone has it, and the sync buttons. */
private fun LazyListScope.desktopMaterials(
    model: RemoteModel,
    state: RemoteState,
    phone: List<Material>,
    onReplaced: (String) -> Unit,
) {
    val status = state.desktop.associate { it.id to syncStatus(phone, state.serverId, it) }
    val pending = state.desktop.filter { status[it.id] != SyncStatus.Synced }
    item {
        Text(
            "用电脑版里设定的音色和语速来读，电脑上已经合成过的句子不会再合成一遍。" +
                "电脑上改过的材料，更新后会替换手机上的旧版本，保留听到的位置。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    item {
        Row(
            Modifier.fillMaxWidth().clickable(enabled = !state.busy) { model.setTranslate(!state.translate) },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Checkbox(state.translate, { model.setTranslate(it) }, enabled = !state.busy)
            Text("英语材料附上中文翻译（电脑联网翻译）")
        }
    }
    item {
        val s = state.syncing
        if (s != null) {
            SyncProgress(model, state, s)
        } else {
            Button(
                onClick = { model.sync(pending, onReplaced) },
                enabled = pending.isNotEmpty() && !state.busy,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(if (pending.isEmpty()) "全部已同步" else "同步全部（${pending.size} 篇未同步或有更新）")
            }
        }
    }
    if (state.desktop.isEmpty()) {
        item {
            Text("电脑版 Repi 的资料库里还没有材料。", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    items(state.desktop, key = { "d${it.id}" }) { m ->
        DesktopRow(m, status[m.id] ?: SyncStatus.Missing, enabled = !state.busy, syncing = state.syncing?.id == m.id) {
            model.sync(listOf(m), onReplaced)
        }
    }
}

@Composable
private fun SyncProgress(model: RemoteModel, state: RemoteState, m: DesktopMaterial) {
    val job = state.job
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    m.title, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.titleSmall,
                )
                val step = when {
                    state.downloading -> "正在传到手机"
                    job == null || job.done == 0 -> "电脑正在准备"
                    else -> "电脑合成中 ${job.done}/${job.total} 句"
                }
                val of = if (state.syncCount > 1) "第 ${state.syncIndex + 1}/${state.syncCount} 篇 · " else ""
                Text(of + step, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedButton(onClick = model::cancel) { Text("停止") }
        }
        if (job != null && job.total > 0 && job.done > 0 && !state.downloading) {
            LinearProgressIndicator({ job.done.toFloat() / job.total }, Modifier.fillMaxWidth())
        } else {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
    }
}

@Composable
private fun DesktopRow(m: DesktopMaterial, status: SyncStatus, enabled: Boolean, syncing: Boolean, onSync: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(m.title, maxLines = 2, overflow = TextOverflow.Ellipsis)
            val lang = if (m.lang == "zh") "中文" else "英语"
            val st = when (status) {
                SyncStatus.Missing -> "未同步"
                SyncStatus.Synced -> "已同步"
                SyncStatus.Changed -> "电脑上改过"
            }
            Text(
                "$lang · ${m.sentences} 句 · $st",
                style = MaterialTheme.typography.bodySmall,
                color = if (status == SyncStatus.Changed) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        when {
            syncing -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            status == SyncStatus.Synced -> TextButton(onClick = onSync, enabled = enabled) { Text("重新同步") }
            status == SyncStatus.Changed -> Button(onClick = onSync, enabled = enabled) { Text("更新") }
            else -> OutlinedButton(onClick = onSync, enabled = enabled) { Text("同步") }
        }
    }
}
