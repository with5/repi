package io.github.with5.repi

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.core.content.IntentCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.with5.repi.ui.ArticleScreen
import io.github.with5.repi.ui.LibraryScreen
import io.github.with5.repi.ui.NewsScreen
import io.github.with5.repi.ui.PlayerScreen
import io.github.with5.repi.ui.RemoteScreen
import io.github.with5.repi.ui.RepiTheme

class MainActivity : ComponentActivity() {
    private val model: PlayerModel by viewModels()
    private val remote: RemoteModel by viewModels()
    private val news: NewsModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) handle(intent)
        setContent {
            RepiTheme {
                val openId by model.openId.collectAsStateWithLifecycle()
                val materials by model.library.materials.collectAsStateWithLifecycle()
                val material = openId?.let { id -> materials.find { it.id == id } }
                var remoteOpen by rememberSaveable { mutableStateOf(false) }
                var newsOpen by rememberSaveable { mutableStateOf(false) }
                val article by news.state.collectAsStateWithLifecycle()
                when {
                    material != null -> {
                        BackHandler { model.close() }
                        PlayerScreen(model, material, onBack = model::close)
                    }
                    remoteOpen -> {
                        BackHandler { remote.stopAudition(); remoteOpen = false }
                        RemoteScreen(
                            remote,
                            onBack = { remote.stopAudition(); remoteOpen = false },
                            pausePlayer = model::pause,
                            onReplaced = model::delete,
                            onDone = { m ->
                                remoteOpen = false
                                model.open(m.id, play = false)
                            },
                        )
                    }
                    newsOpen && article.open != null -> {
                        BackHandler { news.close() }
                        ArticleScreen(
                            news, remote,
                            onBack = news::close,
                            onListen = { m -> model.open(m.id, play = false) },
                        )
                    }
                    newsOpen -> {
                        BackHandler { newsOpen = false }
                        NewsScreen(news, onBack = { newsOpen = false }, onConnect = { remoteOpen = true })
                    }
                    else -> LibraryScreen(model, onRemote = { remoteOpen = true }, onNews = { newsOpen = true })
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    /** Imports the .repi file the app was opened with or shared. */
    private fun handle(intent: Intent?) {
        val uri: Uri? = when (intent?.action) {
            Intent.ACTION_VIEW -> intent.data
            Intent.ACTION_SEND -> IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)
            else -> null
        }
        if (uri != null) model.import(uri)
    }
}
