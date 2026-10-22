// Printdeck for the desktop: a window that shows the deck UI of printdeck app, for a printer on USB.
// The window starts printdeck app, and shows its remote screen (deck.h).
#include <QCoreApplication>
#include <QDir>
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
    engine.load(QUrl(QStringLiteral("qrc:/Main.qml")));

    // -parent: printdeck app ends when the window closes its input.
    QStringList args{QStringLiteral("app"), QStringLiteral("-parent")};
    args += app.arguments().mid(1);

    // With -sim, a second window shows the simulated printer in 3D (Sim3D.qml).
    if (args.contains(QStringLiteral("-sim"))) {
        qmlRegisterType<PathGeometry>("Printdeck.Sim", 1, 0, "PathGeometry");
        qmlRegisterType<MeshGeometry>("Printdeck.Sim", 1, 0, "MeshGeometry");
        engine.rootContext()->setContextProperty(QStringLiteral("sim"), &sim);
        engine.load(QUrl(QStringLiteral("qrc:/Sim3D.qml")));
        QObject::connect(&deck, &Deck::screenChanged, &sim, [&deck, &sim] {
            static bool started = false;
            if (!started && deck.base().isValid()) {
                started = true;
                sim.start(deck.base(), deck.token());
            }
        });
    }
    deck.start(printdeckProgram(), args);
    QObject::connect(&app, &QCoreApplication::aboutToQuit, &deck, &Deck::stop);

    // PRINTDECK_SHOT=FILE.png writes a picture of the windows after PRINTDECK_SHOT_SECONDS (8), and
    // ends the app.
    // It is for the pictures of the manual and for tests.
    QString shot = qEnvironmentVariable("PRINTDECK_SHOT");
    if (!shot.isEmpty()) {
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
            QCoreApplication::quit();
        });
    }
    return app.exec();
}
