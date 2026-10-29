// Printdeck for the desktop: a window that shows the deck UI of printdeck app, for a printer on USB.
// The window starts printdeck app, and shows its remote screen (deck.h). With -deck ADDRESS, it
// shows the remote screen of a Printdeck on the network.
#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QTimer>

#include "deck.h"
#include "sim3d.h"

// printdeckProgram is the path of the printdeck program: $PRINTDECK, or the program next to the
// window program.
static QString printdeckProgram()
{
    QString env = qEnvironmentVariable("PRINTDECK");
    if (!env.isEmpty())
        return env;
#ifdef Q_OS_WIN
    const QString name = QStringLiteral("printdeck.exe");
#else
    const QString name = QStringLiteral("printdeck");
#endif
    return QDir(QCoreApplication::applicationDirPath()).filePath(name);
}

int main(int argc, char *argv[])
{
    QGuiApplication app(argc, argv);
    app.setApplicationName(QStringLiteral("Printdeck"));
    app.setOrganizationName(QStringLiteral("Printdeck"));

    Deck deck;
    SimFeed sim; // before the engine: the QML of the 3D window reads it until the engine ends
    QQmlApplicationEngine engine;
    Factory factory(&deck);
    engine.setNetworkAccessManagerFactory(&factory);
    engine.rootContext()->setContextProperty(QStringLiteral("printdeck"), &deck);
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app,
                     [] { QCoreApplication::exit(1); }, Qt::QueuedConnection);

    // -parent: printdeck app ends when the window closes its input.
    QStringList args{QStringLiteral("app"), QStringLiteral("-parent")};
    args += app.arguments().mid(1);

    // -deck ADDRESS: the window shows the Printdeck at ADDRESS on the network, and starts no app.
    // With -sim, the 3D window shows the simulated printer of that deck.
    QUrl deckUrl;
    if (int i = args.indexOf(QStringLiteral("-deck")); i >= 0 && i + 1 < args.size()) {
        QString a = args.takeAt(i + 1);
        args.removeAt(i);
        deckUrl = QUrl::fromUserInput(a.contains(QStringLiteral("://")) ? a : QStringLiteral("http://") + a);
        if (!deckUrl.isValid() || deckUrl.host().isEmpty()) {
            qWarning("-deck: not an address: %s", qPrintable(a));
            return 2;
        }
        deckUrl.setPath(QString());
    }

    // With -sim, a second window shows the simulated printer in 3D (Sim3D.qml), and the deck
    // window gives it the picture of the deck UI (simOn, sim.screen).
    const bool simOn = args.contains(QStringLiteral("-sim"));
    // The simulated deck shows the screens of the touch screen of a deck.
    deck.setTouchLayout(simOn);
    engine.rootContext()->setContextProperty(QStringLiteral("sim"), &sim);
    engine.rootContext()->setContextProperty(QStringLiteral("simOn"), simOn);
    // PRINTDECK_FILM=OUT.mp4 (with -sim): the 3D window shows the film of desktop/film/, and
    // records it to OUT.mp4 when OUT.mp4.go exists.
    const QString filmPath = qEnvironmentVariable("PRINTDECK_FILM");
    FilmRecorder film(filmPath);
    engine.rootContext()->setContextProperty(QStringLiteral("filmOn"), simOn && !filmPath.isEmpty());
    engine.rootContext()->setContextProperty(QStringLiteral("film"), &film);
    // The pictures of the title cards (film/film.sh): the folder cards next to OUT.mp4.
    engine.rootContext()->setContextProperty(QStringLiteral("filmCards"),
                                             QFileInfo(filmPath).absolutePath() + QStringLiteral("/cards"));
    // PRINTDECK_CAMERA=1 opens the Camera window at the start (Ctrl+K opens it later).
    engine.rootContext()->setContextProperty(QStringLiteral("cameraOn"), qEnvironmentVariableIntValue("PRINTDECK_CAMERA") == 1);
    engine.load(QUrl(QStringLiteral("qrc:/Main.qml")));
    if (simOn) {
        qmlRegisterType<PathGeometry>("Printdeck.Sim", 1, 0, "PathGeometry");
        qmlRegisterType<MeshGeometry>("Printdeck.Sim", 1, 0, "MeshGeometry");
        qmlRegisterType<ImageTexture>("Printdeck.Sim", 1, 0, "ImageTexture");
        engine.load(QUrl(QStringLiteral("qrc:/Sim3D.qml")));
        QObject::connect(&deck, &Deck::screenChanged, &sim, [&deck, &sim] {
            static bool started = false;
            // A deck on the network gives its key after its address: start with the key.
            if (!started && deck.base().isValid() && !deck.token().isEmpty()) {
                started = true;
                sim.start(deck.base(), deck.token());
            }
        });
    }
    // PRINTDECK_ATTACH=FILE: the window connects to a printdeck app that runs already, with the
    // first line of its output in FILE, and starts no app. The app then goes on when the window
    // closes, for example a print of the simulator while the film changes.
    const QString attachFile = qEnvironmentVariable("PRINTDECK_ATTACH");
    QFile hello(attachFile);
    if (deckUrl.isValid()) {
        deck.connectTo(deckUrl);
    } else if (!attachFile.isEmpty()) {
        if (!hello.open(QIODevice::ReadOnly) || !deck.attach(hello.readLine()))
            qWarning("PRINTDECK_ATTACH: no address in %s", qPrintable(attachFile));
    } else {
        deck.start(printdeckProgram(), args);
    }
    QObject::connect(&app, &QCoreApplication::aboutToQuit, &deck, &Deck::stop);

    // PRINTDECK_SHOT=FILE.png writes a picture of the windows after PRINTDECK_SHOT_SECONDS (8), and
    // ends the app. With PRINTDECK_SHOT_FRAMES=N (and -sim), it first writes N pictures of the 3D
    // window, 10 a second, to FILE with "-3d-NNNN" before .png.
    // It is for the pictures of the manual and for tests.
    QString shot = qEnvironmentVariable("PRINTDECK_SHOT");
    int frames = qEnvironmentVariableIntValue("PRINTDECK_SHOT_FRAMES");
    QTimer frameTimer;
    int frame = 0;
    if (!shot.isEmpty() && frames > 0) {
        int seconds = qEnvironmentVariableIntValue("PRINTDECK_SHOT_SECONDS");
        frameTimer.setInterval(100);
        QObject::connect(&frameTimer, &QTimer::timeout, &app, [&] {
            const auto roots = engine.rootObjects();
            if (auto *win = roots.size() > 1 ? qobject_cast<QQuickWindow *>(roots[1]) : nullptr) {
                QString name = shot;
                name.replace(QStringLiteral(".png"), QStringLiteral("-3d-%1.png").arg(frame, 4, 10, QLatin1Char('0')));
                win->grabWindow().save(name);
            }
            if (++frame >= frames)
                QCoreApplication::quit();
        });
        QTimer::singleShot((seconds > 0 ? seconds : 8) * 1000, &frameTimer, qOverload<>(&QTimer::start));
    } else if (!shot.isEmpty()) {
        int seconds = qEnvironmentVariableIntValue("PRINTDECK_SHOT_SECONDS");
        QTimer::singleShot((seconds > 0 ? seconds : 8) * 1000, &app, [&engine, shot] {
            // The deck window to FILE, the 3D window (with -sim) to FILE with "-3d" before .png.
            const auto roots = engine.rootObjects();
            for (int i = 0; i < roots.size(); i++) {
                auto *win = qobject_cast<QQuickWindow *>(roots[i]);
                if (!win)
                    continue;
                QString name = shot;
                if (i > 0)
                    name.replace(QStringLiteral(".png"), QStringLiteral("-3d.png"));
                win->grabWindow().save(name);
            }
            // The Camera window, when it is open, to FILE with "-camera" before .png.
            for (QWindow *w : QGuiApplication::topLevelWindows()) {
                auto *win = qobject_cast<QQuickWindow *>(w);
                if (win && win->isVisible() && win->title() == QStringLiteral("Printdeck camera")) {
                    QString name = shot;
                    win->grabWindow().save(name.replace(QStringLiteral(".png"), QStringLiteral("-camera.png")));
                }
            }
            QCoreApplication::quit();
        });
    }
    return app.exec();
}
