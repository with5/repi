package io.github.with5.repi

import android.app.Application
import android.content.Context
import androidx.core.content.edit
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import java.time.Duration
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** A blog the computer follows. */
@Serializable
data class NewsSource(val id: String, val name: String, val url: String = "")

/** A post of a blog. */
@Serializable
data class NewsItem(
    val source: String,
    val title: String,
    val url: String,
    val summary: String = "",
    val published: String = "",
)

@Serializable
data class NewsResult(val items: List<NewsItem> = emptyList(), val errors: Map<String, String> = emptyMap())

@Serializable
data class ArticleText(val title: String = "", val text: String = "")

/** When [published] was, for the list: "今天", "3 天前", or the date. */
fun ago(published: String, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
    val t = runCatching { OffsetDateTime.parse(published).toInstant() }.getOrNull() ?: return ""
    if (t.epochSecond <= 0) return ""
    val day = t.atZone(zone).toLocalDate()
    val today = now.atZone(zone).toLocalDate()
    val days = Duration.between(day.atStartOfDay(zone), today.atStartOfDay(zone)).toDays()
    return when {
        days <= 0 -> "今天"
        days == 1L -> "昨天"
        days < 7 -> "$days 天前"
        day.year == today.year -> day.format(DateTimeFormatter.ofPattern("M 月 d 日"))
        else -> day.format(DateTimeFormatter.ofPattern("yyyy 年 M 月 d 日"))
    }
}

/** What the news screens remember: the source shown, the posts read and those made into materials. */
private class NewsPrefs(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("news", Context.MODE_PRIVATE)
    var source: String
        get() = prefs.getString("source", "") ?: ""
        set(v) = prefs.edit { putString("source", v) }
    var read: Set<String>
        get() = prefs.getStringSet("read", emptySet()).orEmpty()
        set(v) = prefs.edit { putStringSet("read", v.toList().takeLast(MAX_READ).toSet()) }
    fun material(url: String): String? = prefs.getString("m:$url", null)
    fun setMaterial(url: String, id: String) = prefs.edit { putString("m:$url", id) }

    companion object {
        const val MAX_READ = 2000
    }
}

data class NewsState(
    val sources: List<NewsSource> = emptyList(),
    /** The source shown; "" is all of them. */
    val source: String = "",
    val items: List<NewsItem> = emptyList(),
    val errors: Map<String, String> = emptyMap(),
    val loading: Boolean = false,
    val error: String? = null,
    val read: Set<String> = emptySet(),
    /** The post open for reading, its text once fetched, and why it could not be. */
    val open: NewsItem? = null,
    val article: ArticleText? = null,
    val articleLoading: Boolean = false,
    val articleError: String? = null,
) {
    fun sourceName(id: String) = sources.find { it.id == id }?.name ?: id
}

/** The 资讯 screens: the AI labs' posts, fetched by the computer. */
class NewsModel(app: Application) : AndroidViewModel(app) {
    private val prefs = NewsPrefs(app)
    private val remote = RemoteSettings(app)
    private val library = Library.get(app)

    private val _state = MutableStateFlow(NewsState(source = prefs.source, read = prefs.read))
    val state: StateFlow<NewsState> = _state.asStateFlow()

    private var load: Job? = null
    private var fetch: Job? = null

    val connected get() = remote.url.isNotBlank() && remote.token.isNotBlank()

    private fun client() = RemoteClient(remote.url, remote.token)

    /** Loads the posts unless they are there; [refresh] has the computer fetch them anew. */
    fun load(refresh: Boolean = false) {
        if (!connected) return
        if (!refresh && (_state.value.items.isNotEmpty() || load?.isActive == true)) return
        load?.cancel()
        val c = client()
        _state.update { it.copy(loading = true, error = null) }
        load = viewModelScope.launch {
            try {
                if (_state.value.sources.isEmpty()) {
                    val s = c.newsSources()
                    _state.update { it.copy(sources = s) }
                }
                val r = c.news(emptyList(), refresh)
                _state.update { it.copy(loading = false, items = r.items, errors = r.errors) }
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                _state.update { it.copy(loading = false, error = e.message ?: e.javaClass.simpleName) }
            }
        }
    }

    fun setSource(id: String) {
        prefs.source = id
        _state.update { it.copy(source = id) }
    }

    /** Opens [item] for reading and has the computer extract its text. */
    fun open(item: NewsItem) {
        val read = _state.value.read + item.url
        prefs.read = read
        _state.update { it.copy(open = item, article = null, articleError = null, articleLoading = true, read = read) }
        fetch?.cancel()
        val c = client()
        fetch = viewModelScope.launch {
            try {
                val a = c.article(item.url)
                _state.update { if (it.open == item) it.copy(article = a, articleLoading = false) else it }
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                _state.update {
                    if (it.open == item) it.copy(articleLoading = false, articleError = e.message ?: e.javaClass.simpleName) else it
                }
            }
        }
    }

    fun close() {
        fetch?.cancel()
        _state.update { it.copy(open = null, article = null, articleError = null, articleLoading = false) }
    }

    /** The material made of the post at [url], if it is still on the phone. */
    fun materialOf(url: String): Material? = prefs.material(url)?.let { library.get(it) }

    fun madeInto(url: String, m: Material) = prefs.setMaterial(url, m.id)
}
