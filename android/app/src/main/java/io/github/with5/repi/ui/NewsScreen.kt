package io.github.with5.repi.ui

import android.os.Build
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.with5.repi.Material
import io.github.with5.repi.NewsItem
import io.github.with5.repi.NewsModel
import io.github.with5.repi.NewsState
import io.github.with5.repi.R
import io.github.with5.repi.RemoteModel
import io.github.with5.repi.ago

/** The AI labs' latest posts, newest first, like a news app. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewsScreen(model: NewsModel, onBack: () -> Unit, onConnect: () -> Unit) {
    val state by model.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { model.load() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("资讯") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(painterResource(R.drawable.ic_back), "返回") } },
                actions = {
                    if (model.connected) {
                        IconButton(onClick = { model.load(refresh = true) }, enabled = !state.loading) {
                            Icon(painterResource(R.drawable.ic_refresh), "刷新")
                        }
                    }
                },
            )
        },
    ) { padding ->
        if (!model.connected) {
            Column(
                Modifier.fillMaxSize().padding(padding).padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(
                    "资讯由电脑去各家博客抓取，先连上电脑上的 repi serve。",
                    textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.size(16.dp))
                Button(onClick = onConnect) { Text("连接电脑") }
            }
            return@Scaffold
        }
        Column(Modifier.fillMaxSize().padding(top = padding.calculateTopPadding())) {
            SourceChips(state, model::setSource)
            PullToRefreshBox(
                isRefreshing = state.loading && state.items.isNotEmpty(),
                onRefresh = { model.load(refresh = true) },
                modifier = Modifier.fillMaxSize(),
            ) {
                NewsList(model, state, bottom = padding.calculateBottomPadding())
            }
        }
    }
}

@Composable
private fun SourceChips(state: NewsState, onSelect: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        FilterChip(state.source == "", { onSelect("") }, label = { Text("全部") })
        for (s in state.sources) {
            FilterChip(state.source == s.id, { onSelect(s.id) }, label = { Text(s.name) })
        }
    }
}

@Composable
private fun NewsList(model: NewsModel, state: NewsState, bottom: androidx.compose.ui.unit.Dp) {
    val shown = state.items.filter { state.source == "" || it.source == state.source }
    val failed = state.errors.filterKeys { state.source == "" || it == state.source }
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = bottom + 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (state.error != null) {
            item { Text("拿不到资讯：${state.error}", color = MaterialTheme.colorScheme.error) }
        }
        for ((id, msg) in failed) {
            item(key = "err-$id") {
                Text(
                    "${state.sourceName(id)} 暂时拿不到：$msg",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error,
                )
            }
        }
        if (state.loading && state.items.isEmpty()) {
            item {
                Box(Modifier.fillMaxWidth().padding(48.dp), contentAlignment = Alignment.Center) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        CircularProgressIndicator()
                        Spacer(Modifier.size(12.dp))
                        Text("电脑正在抓取各家博客…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        items(shown, key = { it.url }) { item ->
            NewsCard(item, state.sourceName(item.source), read = item.url in state.read, listened = model.materialOf(item.url) != null) {
                model.open(item)
            }
        }
    }
}

@Composable
private fun NewsCard(item: NewsItem, source: String, read: Boolean, listened: Boolean, onClick: () -> Unit) {
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            val meta = listOfNotNull(source, ago(item.published).ifEmpty { null }, if (listened) "已加入材料" else null)
            Text(
                meta.joinToString(" · "),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            Text(
                item.title,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = if (read) FontWeight.Normal else FontWeight.SemiBold,
                color = if (read) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                maxLines = 3, overflow = TextOverflow.Ellipsis,
            )
            if (item.summary.isNotBlank()) {
                Text(
                    item.summary,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2, overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** One post, read on the phone, with a button to have the computer read it aloud. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArticleScreen(model: NewsModel, remote: RemoteModel, onBack: () -> Unit, onListen: (Material) -> Unit) {
    val state by model.state.collectAsStateWithLifecycle()
    val rs by remote.state.collectAsStateWithLifecycle()
    val item = state.open ?: return
    val uri = LocalUriHandler.current
    val snackbar = remember { SnackbarHostState() }
    val existing = model.materialOf(item.url)
    // the page itself; the text the computer extracted is a tap away, and
    // is what the computer reads
    var reader by rememberSaveable(item.url) { mutableStateOf(false) }
    val page = rememberWebPage(item.url)

    LaunchedEffect(rs.error) {
        rs.error?.let {
            snackbar.showSnackbar(it)
            remote.errorShown()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(state.sourceName(item.source), maxLines = 1) },
                navigationIcon = { IconButton(onClick = onBack) { Icon(painterResource(R.drawable.ic_back), "返回") } },
                actions = {
                    TextButton(onClick = { reader = !reader }) { Text(if (reader) "网页" else "正文") }
                    IconButton(onClick = { uri.openUri(item.url) }) { Icon(painterResource(R.drawable.ic_open), "在浏览器里打开") }
                },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            val text = state.article?.text
            run {
                Surface(tonalElevation = 3.dp) {
                    Column(Modifier.fillMaxWidth().navigationBarsPadding().padding(16.dp, 12.dp)) {
                        ListenBar(remote, existing, enabled = text != null, onOpen = onListen) {
                            val title = item.title.ifBlank { state.article?.title.orEmpty() }
                            remote.make(title, text!!) { m ->
                                model.madeInto(item.url, m)
                                onListen(m)
                            }
                        }
                        if (existing == null && text == null && !rs.busy) {
                            Text(
                                if (state.articleLoading) "电脑正在提取正文…" else "没能提取正文：${state.articleError.orEmpty()}",
                                style = MaterialTheme.typography.bodySmall,
                                color = if (state.articleLoading) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.error,
                                modifier = Modifier.padding(top = 6.dp),
                            )
                        } else if (existing == null && rs.voices.isNotEmpty()) {
                            val voice = rs.voices.find { it.name == rs.voice }?.label ?: rs.voice
                            Text(
                                "用 $voice、${rs.speed}x 读" + if (rs.translate) "，附中文翻译" else "",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(top = 6.dp),
                            )
                        }
                    }
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            // kept while the text is shown, so going back to it does not load it again
            WebPage(page, Modifier.fillMaxSize(), onReader = { reader = true })
            if (reader) {
                Surface(Modifier.fillMaxSize()) { ReaderText(model, state, item) }
            }
        }
    }
}

/** The text the computer extracted from [item]. */
@Composable
private fun ReaderText(model: NewsModel, state: NewsState, item: NewsItem) {
    val uri = LocalUriHandler.current
    run {
        SelectionContainer {
            Column(
                Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                ago(item.published).takeIf { it.isNotEmpty() }?.let {
                    Text(it, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                }
                Text(item.title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold)
                when {
                    state.articleLoading -> Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                        Spacer(Modifier.size(12.dp))
                        Text("电脑正在提取正文…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    state.articleError != null -> Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("没能提取正文：${state.articleError}", color = MaterialTheme.colorScheme.error)
                        Row {
                            OutlinedButton(onClick = { model.open(item) }) { Text("重试") }
                            Spacer(Modifier.size(8.dp))
                            TextButton(onClick = { uri.openUri(item.url) }) { Text("在浏览器里打开") }
                        }
                    }
                    else -> state.article?.text?.split("\n\n")?.forEach { p ->
                        Text(p, style = MaterialTheme.typography.bodyLarge)
                    }
                }
            }
        }
    }
}

@Composable
private fun ListenBar(remote: RemoteModel, existing: Material?, enabled: Boolean, onOpen: (Material) -> Unit, onMake: () -> Unit) {
    val rs by remote.state.collectAsStateWithLifecycle()
    val job = rs.job
    when {
        rs.downloading -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("正在传到手机…")
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        job != null -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(if (job.done == 0) "电脑正在准备…" else "电脑合成中 ${job.done}/${job.total} 句", Modifier.weight(1f))
                OutlinedButton(onClick = remote::cancel) { Text("取消") }
            }
            if (job.total > 0 && job.done > 0) {
                LinearProgressIndicator({ job.done.toFloat() / job.total }, Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        }
        existing != null -> Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "已经加入材料", Modifier.weight(1f),
                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(onClick = { onOpen(existing) }) { Text("去听") }
        }
        else -> Button(onClick = onMake, enabled = enabled && !rs.busy && rs.voices.isNotEmpty(), modifier = Modifier.fillMaxWidth()) {
            Text("让电脑读，加入材料")
        }
    }
}

/** A web view showing one page, and what it is doing. */
private class WebPageState(val view: WebView) {
    var progress by mutableIntStateOf(0)
    var canGoBack by mutableStateOf(false)
    var error by mutableStateOf<String?>(null)
}

@Composable
private fun rememberWebPage(url: String): WebPageState {
    val context = LocalContext.current
    val page = remember(url) {
        val view = WebView(context)
        WebPageState(view).also { p ->
            view.settings.javaScriptEnabled = true
            view.settings.domStorageEnabled = true
            if (Build.VERSION.SDK_INT >= 33) view.settings.isAlgorithmicDarkeningAllowed = true
            view.webViewClient = object : WebViewClient() {
                override fun doUpdateVisitedHistory(v: WebView, u: String?, isReload: Boolean) {
                    p.canGoBack = v.canGoBack()
                }

                override fun onPageStarted(v: WebView, u: String?, favicon: android.graphics.Bitmap?) {
                    p.error = null
                }

                override fun onReceivedError(v: WebView, req: WebResourceRequest, err: WebResourceError) {
                    if (req.isForMainFrame) p.error = err.description.toString()
                }

                // links to apps (mailto:, intent:…) are not followed
                override fun shouldOverrideUrlLoading(v: WebView, req: WebResourceRequest): Boolean =
                    req.url.scheme != "http" && req.url.scheme != "https"
            }
            view.webChromeClient = object : WebChromeClient() {
                override fun onProgressChanged(v: WebView, newProgress: Int) {
                    p.progress = newProgress
                }
            }
            view.loadUrl(url)
        }
    }
    DisposableEffect(page) { onDispose { page.view.destroy() } }
    return page
}

@Composable
private fun WebPage(page: WebPageState, modifier: Modifier, onReader: () -> Unit) {
    // back goes back in the page first, then leaves it
    BackHandler(enabled = page.canGoBack) { page.view.goBack() }
    Box(modifier) {
        AndroidView({ page.view }, Modifier.fillMaxSize())
        if (page.progress < 100) {
            LinearProgressIndicator({ page.progress / 100f }, Modifier.fillMaxWidth())
        }
        page.error?.let { err ->
            Surface(Modifier.fillMaxSize()) {
                Column(
                    Modifier.padding(32.dp),
                    verticalArrangement = Arrangement.Center,
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text("手机打不开这个网页：$err", textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.error)
                    Spacer(Modifier.size(8.dp))
                    Text(
                        "可以看电脑提取的正文。",
                        textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(Modifier.size(16.dp))
                    Row {
                        Button(onClick = onReader) { Text("看正文") }
                        Spacer(Modifier.size(8.dp))
                        OutlinedButton(onClick = { page.error = null; page.view.reload() }) { Text("重试") }
                    }
                }
            }
        }
    }
}
