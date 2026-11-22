package io.github.with5.repi

import android.content.Context
import android.content.SharedPreferences
import android.net.Uri
import androidx.core.content.edit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.io.File

/** What to do at the end of a sentence, as on the desktop. */
enum class Mode { LoopAll, RepeatEach }

/** An imported pack. [id] is its folder's name. */
data class Material(val id: String, val dir: File, val manifest: Manifest, val added: Long) {
    fun audio(i: Int): File = File(dir, manifest.sentences[i].audio)
}

/** Where the listener is in a material. */
data class Progress(val index: Int = 0, val mode: Mode = Mode.LoopAll, val repeat: Int = 3)

/** The imported materials, kept in the app's files, and where each was left. */
class Library private constructor(context: Context) {
    private val app = context.applicationContext
    private val dir = File(app.filesDir, "packs")
    private val prefs: SharedPreferences = app.getSharedPreferences("progress", Context.MODE_PRIVATE)

    private val _materials = MutableStateFlow<List<Material>>(emptyList())
    val materials: StateFlow<List<Material>> = _materials.asStateFlow()

    init {
        reload()
    }

    private fun reload() {
        val list = dir.listFiles { f -> f.isDirectory }.orEmpty().mapNotNull { d ->
            val f = File(d, "manifest.json")
            runCatching { Material(d.name, d, parseManifest(f.readText()), f.lastModified()) }.getOrNull()
        }
        _materials.value = list.sortedByDescending { lastOpened(it.id).coerceAtLeast(it.added) }
    }

    fun get(id: String): Material? = _materials.value.find { it.id == id }

    /** Imports the pack at [uri]; returns the new material. */
    suspend fun import(uri: Uri): Material = withContext(Dispatchers.IO) {
        val input = app.contentResolver.openInputStream(uri) ?: throw PackException("无法读取这个文件")
        val d = input.use { importPack(it, dir) }
        reload()
        get(d.name) ?: throw PackException("导入失败")
    }

    suspend fun delete(id: String) = withContext(Dispatchers.IO) {
        File(dir, id).deleteRecursively()
        prefs.edit { remove("$id.index"); remove("$id.mode"); remove("$id.repeat"); remove("$id.opened") }
        reload()
    }

    fun progress(id: String) = Progress(
        index = prefs.getInt("$id.index", 0),
        mode = Mode.entries.getOrElse(prefs.getInt("$id.mode", 0)) { Mode.LoopAll },
        repeat = prefs.getInt("$id.repeat", 3).coerceIn(1, MAX_REPEAT),
    )

    fun saveProgress(id: String, p: Progress) = prefs.edit {
        putInt("$id.index", p.index)
        putInt("$id.mode", p.mode.ordinal)
        putInt("$id.repeat", p.repeat)
    }

    /** Gives material [to] the progress of [from], which it replaces. */
    fun moveProgress(from: String, to: String) {
        val p = progress(from)
        val n = get(to)?.manifest?.sentences?.size ?: 1
        saveProgress(to, p.copy(index = p.index.coerceIn(0, n - 1)))
        val opened = lastOpened(from)
        prefs.edit {
            if (opened > 0) putLong("$to.opened", opened)
            if (prefs.getString("last", null) == from) putString("last", to)
        }
    }

    fun saveIndex(id: String, index: Int) = prefs.edit { putInt("$id.index", index) }

    private fun lastOpened(id: String) = prefs.getLong("$id.opened", 0)

    /** Records that [id] was opened, which puts it first in the list. */
    fun markOpened(id: String) {
        prefs.edit { putLong("$id.opened", System.currentTimeMillis()); putString("last", id) }
        _materials.value = _materials.value.sortedByDescending { lastOpened(it.id).coerceAtLeast(it.added) }
    }

    /** The material played last, which the headset's play button resumes. */
    val last: String? get() = prefs.getString("last", null)

    companion object {
        const val MAX_REPEAT = 10

        @Volatile private var instance: Library? = null

        fun get(context: Context): Library =
            instance ?: synchronized(this) { instance ?: Library(context).also { instance = it } }
    }
}

/** Settings shared by all materials. */
class Settings(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("settings", Context.MODE_PRIVATE)
    var speed: Float
        get() = prefs.getFloat("speed", 1f)
        set(v) = prefs.edit { putFloat("speed", v) }
    var showTranslation: Boolean
        get() = prefs.getBoolean("translation", false)
        set(v) = prefs.edit { putBoolean("translation", v) }
    var hideText: Boolean
        get() = prefs.getBoolean("hideText", false)
        set(v) = prefs.edit { putBoolean("hideText", v) }
}
