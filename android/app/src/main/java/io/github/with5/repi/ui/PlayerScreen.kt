package io.github.with5.repi.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.with5.repi.Library
import io.github.with5.repi.Material
import io.github.with5.repi.Mode
import io.github.with5.repi.PlayState
import io.github.with5.repi.PlayerModel
import io.github.with5.repi.R
import io.github.with5.repi.Sentence
import io.github.with5.repi.tokenAt

private val speeds = listOf(0.7f, 0.85f, 1f, 1.15f, 1.3f)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PlayerScreen(model: PlayerModel, material: Material, onBack: () -> Unit) {
    val state by model.state.collectAsStateWithLifecycle()
    val showTrans by model.showTranslation.collectAsStateWithLifecycle()
    val hideText by model.hideText.collectAsStateWithLifecycle()
    val sentences = material.manifest.sentences
    val isOpen = state.materialId == material.id
    val current = (if (isOpen) state.index else model.library.progress(material.id).index)
        .coerceIn(0, sentences.size - 1)
    val zh = material.manifest.lang == "zh"
    val onPlay = { if (isOpen) model.togglePlay() else model.open(material.id) }

    val list = rememberLazyListState()
    val posMs = if (isOpen) state.positionMs else -1
    FollowSpeech(list, sentences, current, posMs)

    val toggles = @Composable {
        if (!zh) {
            FilterChip(showTrans, onClick = { model.setShowTranslation(!showTrans) }, label = { Text("翻译") })
        }
        FilterChip(hideText, onClick = { model.setHideText(!hideText) }, label = { Text("隐藏原文") })
    }
    val sentenceList = @Composable { modifier: Modifier ->
        LazyColumn(
            modifier,
            state = list,
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            itemsIndexed(sentences) { i, s ->
                SentenceRow(
                    i, s,
                    current = i == current,
                    posMs = if (i == current) posMs else -1,
                    showTrans = showTrans && !zh,
                    hideText = hideText,
                    onClick = { if (isOpen) model.seekTo(i) else model.open(material.id, index = i) },
                )
            }
        }
    }

    BoxWithConstraints {
        // Landscape (or a tablet held so): the list beside the controls.
        val wide = maxWidth > maxHeight && maxWidth >= 560.dp
        Scaffold(
            topBar = {
                if (!wide) TopAppBar(
                    title = { Text(material.manifest.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    navigationIcon = {
                        IconButton(onClick = onBack) { Icon(painterResource(R.drawable.ic_back), "返回") }
                    },
                )
            },
            bottomBar = {
                if (!wide) {
                    Surface(tonalElevation = 3.dp) {
                        Controls(
                            model, state, isOpen, sentences.size, current, onPlay,
                            Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 16.dp, vertical = 8.dp),
                        )
                    }
                }
            },
        ) { padding ->
            if (wide) {
                // No top bar: the height is short, so the title goes in the panel.
                Row(Modifier.fillMaxSize().padding(padding)) {
                    sentenceList(Modifier.weight(1f).fillMaxHeight())
                    VerticalDivider()
                    Column(
                        Modifier.width(340.dp).fillMaxHeight().verticalScroll(rememberScrollState())
                            .navigationBarsPadding().padding(horizontal = 16.dp, vertical = 8.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            IconButton(onClick = onBack) { Icon(painterResource(R.drawable.ic_back), "返回") }
                            Text(
                                material.manifest.title, style = MaterialTheme.typography.titleMedium,
                                maxLines = 2, overflow = TextOverflow.Ellipsis,
                            )
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { toggles() }
                        Spacer(Modifier.weight(1f))
                        Controls(model, state, isOpen, sentences.size, current, onPlay, Modifier.fillMaxWidth())
                    }
                }
            } else {
                Column(Modifier.fillMaxSize().padding(padding)) {
                    Row(
                        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) { toggles() }
                    sentenceList(Modifier.fillMaxSize())
                }
            }
        }
    }
}

/**
 * Keeps the sentence being read in view: a new sentence is brought to the
 * top; one taller than the screen then scrolls with the words being said, so
 * that its end and its translation come into view as it finishes.
 */
@Composable
private fun FollowSpeech(list: LazyListState, sentences: List<Sentence>, current: Int, posMs: Long) {
    LaunchedEffect(current) {
        list.animateScrollToItem(current)
    }
    val s = sentences[current]
    val n = s.tokens.size
    val step = if (posMs < 0 || n == 0) -1 else (s.tokenAt(posMs) + 1) * 10 / n // tenths read
    LaunchedEffect(current, step) {
        if (step < 0 || list.isScrollInProgress) return@LaunchedEffect
        val info = list.layoutInfo.visibleItemsInfo.find { it.index == current } ?: return@LaunchedEffect
        val viewport = list.layoutInfo.viewportEndOffset - list.layoutInfo.viewportStartOffset
        if (info.size <= viewport) return@LaunchedEffect
        // the words being said sit a third of the way down
        val target = (info.size * step / 10f - viewport / 3f).toInt().coerceIn(0, info.size - viewport)
        list.animateScrollToItem(current, target)
    }
}

/** The current sentence's size: smaller for long ones, so more of it fits. */
private fun bigSize(s: Sentence): TextUnit = when {
    s.text.length > 220 -> 19.sp
    s.text.length > 120 -> 21.sp
    else -> 24.sp
}

@Composable
private fun SentenceRow(
    i: Int, s: Sentence, current: Boolean, posMs: Long,
    showTrans: Boolean, hideText: Boolean, onClick: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Surface(
        Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(12.dp),
        color = if (current) colors.secondaryContainer else Color.Transparent,
    ) {
        Row(Modifier.clickable(onClick = onClick).padding(horizontal = 12.dp, vertical = if (current) 14.dp else 8.dp)) {
            Text(
                "${i + 1}", Modifier.width(32.dp),
                style = MaterialTheme.typography.labelSmall, color = colors.onSurfaceVariant,
            )
            Column(Modifier.weight(1f)) {
                val size = bigSize(s)
                when {
                    hideText -> Text("· · ·", color = colors.onSurfaceVariant, fontSize = if (current) size else 16.sp)
                    current -> Text(
                        karaoke(s, posMs, colors.primary, colors.onSecondaryContainer),
                        fontSize = size, lineHeight = size * 1.4f,
                    )
                    else -> Text(s.text, fontSize = 16.sp, color = colors.onSurface.copy(alpha = 0.75f))
                }
                if (current && showTrans && !s.translation.isNullOrBlank()) {
                    Spacer(Modifier.height(10.dp))
                    Text(s.translation, fontSize = 17.sp, lineHeight = 26.sp, color = colors.onSecondaryContainer.copy(alpha = 0.8f))
                }
            }
        }
    }
}

/** The sentence with the word being said in [accent], the words to come dimmed. */
private fun karaoke(s: Sentence, posMs: Long, accent: Color, text: Color): AnnotatedString {
    if (s.tokens.isEmpty()) return AnnotatedString(s.text)
    val active = if (posMs < 0) -1 else s.tokenAt(posMs)
    return buildAnnotatedString {
        s.tokens.forEachIndexed { i, t ->
            if (t.space) append(' ')
            val style = when {
                active < 0 -> SpanStyle(color = text)
                i == active -> SpanStyle(color = accent, fontWeight = FontWeight.SemiBold)
                i < active -> SpanStyle(color = text)
                else -> SpanStyle(color = text.copy(alpha = 0.4f))
            }
            withStyle(style) { append(t.text) }
        }
    }
}

/** Two rows: the loop mode with its repeat count, then speed, transport and place. */
@Composable
private fun Controls(
    model: PlayerModel, state: PlayState, isOpen: Boolean, total: Int, index: Int,
    onPlay: () -> Unit, modifier: Modifier,
) {
    val mode = state.mode
    val repeat = state.repeat
    Column(modifier, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SingleChoiceSegmentedButtonRow(Modifier.weight(1f)) {
                SegmentedButton(
                    selected = mode == Mode.LoopAll,
                    onClick = { model.setMode(Mode.LoopAll, repeat) },
                    shape = SegmentedButtonDefaults.itemShape(0, 2),
                    icon = {},
                ) { Text("整篇循环", maxLines = 1) }
                SegmentedButton(
                    selected = mode == Mode.RepeatEach,
                    onClick = { model.setMode(Mode.RepeatEach, repeat) },
                    shape = SegmentedButtonDefaults.itemShape(1, 2),
                    icon = {},
                ) {
                    Text(if (mode == Mode.RepeatEach) "第 ${minOf(state.rep + 1, repeat)}/$repeat 遍" else "每句重复", maxLines = 1)
                }
            }
            if (mode == Mode.RepeatEach) {
                IconButton(
                    onClick = { model.setMode(mode, (repeat - 1).coerceAtLeast(1)) },
                    enabled = repeat > 1, modifier = Modifier.size(40.dp),
                ) { Text("−", fontSize = 20.sp) }
                Text("${repeat}遍", style = MaterialTheme.typography.bodyMedium)
                IconButton(
                    onClick = { model.setMode(mode, (repeat + 1).coerceAtMost(Library.MAX_REPEAT)) },
                    enabled = repeat < Library.MAX_REPEAT, modifier = Modifier.size(40.dp),
                ) { Text("+", fontSize = 20.sp) }
            }
        }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            val next = speeds[(speeds.indexOf(state.speed).coerceAtLeast(0) + 1) % speeds.size]
            TextButton(onClick = { model.setSpeed(next) }, modifier = Modifier.width(72.dp)) { Text(speedLabel(state.speed)) }
            Spacer(Modifier.weight(1f))
            IconButton(onClick = { model.previous() }) { Icon(painterResource(R.drawable.ic_previous), "上一句") }
            Spacer(Modifier.width(12.dp))
            val playing = state.playing && isOpen
            FilledIconButton(onClick = onPlay, modifier = Modifier.size(60.dp)) {
                Icon(
                    painterResource(if (playing) R.drawable.ic_pause else R.drawable.ic_play),
                    if (playing) "暂停" else "播放", Modifier.size(30.dp),
                )
            }
            Spacer(Modifier.width(12.dp))
            IconButton(onClick = { model.next() }) { Icon(painterResource(R.drawable.ic_next), "下一句") }
            Spacer(Modifier.weight(1f))
            Text(
                "${index + 1}/$total", Modifier.width(72.dp),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
        }
    }
}

private fun speedLabel(v: Float) = if (v == 1f) "1.0x" else "${v}x"
