package io.github.with5.repi.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.with5.repi.Material
import io.github.with5.repi.PlayerModel
import io.github.with5.repi.R

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LibraryScreen(model: PlayerModel, onRemote: () -> Unit, onNews: () -> Unit) {
    val materials by model.library.materials.collectAsStateWithLifecycle()
    val state by model.state.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    var confirmDelete by remember { mutableStateOf<Material?>(null) }

    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) model.import(uri)
    }

    LaunchedEffect(message) {
        message?.let {
            snackbar.showSnackbar(it)
            model.messageShown()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Repi") },
                actions = {
                    TextButton(onClick = onNews) {
                        Icon(painterResource(R.drawable.ic_news), null)
                        Spacer(Modifier.width(6.dp))
                        Text("资讯")
                    }
                    TextButton(onClick = onRemote) {
                        Icon(painterResource(R.drawable.ic_computer), null)
                        Spacer(Modifier.width(6.dp))
                        Text("连接电脑")
                    }
                },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
        floatingActionButton = {
            ExtendedFloatingActionButton(
                onClick = { pick.launch(arrayOf("*/*")) },
                icon = { Icon(painterResource(R.drawable.ic_add), null) },
                text = { Text("导入") },
            )
        },
        bottomBar = {
            val playing = state.materialId?.let { id -> materials.find { it.id == id } }
            if (playing != null) MiniPlayer(model, playing, state.playing, state.index)
        },
    ) { padding ->
        if (materials.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(padding).padding(32.dp), contentAlignment = Alignment.Center) {
                Text(
                    "还没有材料。\n\n在电脑上的 Repi 里打开一篇材料，点「导出到手机」，" +
                        "把生成的 .repi 文件发到手机上，再用 Repi 打开，或点右下角的「导入」。\n\n" +
                        "也可以在电脑上运行 repi serve，点右上角的「连接电脑」，同步电脑上的材料，或粘贴文字让电脑来读。",
                    textAlign = TextAlign.Center,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            return@Scaffold
        }
        // one column on a phone held upright, more side by side in landscape
        LazyVerticalGrid(
            columns = GridCells.Adaptive(minSize = 300.dp),
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(
                start = 16.dp, end = 16.dp,
                top = padding.calculateTopPadding() + 8.dp,
                bottom = padding.calculateBottomPadding() + 88.dp, // clear of the button
            ),
            verticalArrangement = Arrangement.spacedBy(8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(materials, key = { it.id }) { m ->
                MaterialCard(
                    m,
                    index = if (m.id == state.materialId) state.index else model.library.progress(m.id).index,
                    current = m.id == state.materialId,
                    onClick = { model.open(m.id) },
                    onLongClick = { confirmDelete = m },
                )
            }
        }
    }

    confirmDelete?.let { m ->
        AlertDialog(
            onDismissRequest = { confirmDelete = null },
            title = { Text("删除材料") },
            text = { Text("从手机上删除「${m.manifest.title}」？电脑上的不受影响。") },
            confirmButton = {
                TextButton(onClick = { model.delete(m.id); confirmDelete = null }) { Text("删除") }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = null }) { Text("取消") } },
        )
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun MaterialCard(m: Material, index: Int, current: Boolean, onClick: () -> Unit, onLongClick: () -> Unit) {
    val n = m.manifest.sentences.size
    Card(
        Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick),
        colors = if (current) {
            CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer)
        } else {
            CardDefaults.cardColors()
        },
    ) {
        Column(Modifier.padding(16.dp)) {
            Text(
                m.manifest.title, style = MaterialTheme.typography.titleMedium,
                maxLines = 2, overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.padding(2.dp))
            val lang = if (m.manifest.lang == "zh") "中文" else "英语"
            Text(
                "$lang · $n 句 · 听到第 ${(index + 1).coerceAtMost(n)} 句",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun MiniPlayer(model: PlayerModel, m: Material, playing: Boolean, index: Int) {
    Surface(tonalElevation = 3.dp, onClick = { model.open(m.id, play = false) }) {
        Row(
            Modifier.fillMaxWidth().navigationBarsPadding()
                .padding(start = 16.dp, end = 8.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(m.manifest.title, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    m.manifest.sentences.getOrNull(index)?.text.orEmpty(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
            Spacer(Modifier.width(8.dp))
            IconButton(onClick = model::togglePlay) {
                Icon(painterResource(if (playing) R.drawable.ic_pause else R.drawable.ic_play), if (playing) "暂停" else "播放")
            }
        }
    }
}
