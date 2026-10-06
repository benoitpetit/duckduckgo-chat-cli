# Dashboard local d’utilisation — spécification de conception

## Objectif

Ajouter à DuckDuckGo Chat CLI un dashboard web local qui présente les statistiques d’utilisation en temps réel, leur historique et des analyses IA facultatives. Le dashboard doit rester disponible depuis le processus CLI, fonctionner hors de l’API REST existante et ne pas exposer de port sur le réseau. Il doit aussi permettre de parcourir les conversations archivées, de les consulter et de copier la commande CLI pour les reprendre.

Le travail comprend une page web de référence des commandes et une correction du contraste des suggestions de commandes dans le terminal.

## Besoins validés

- Le serveur web est indépendant du serveur API et écoute seulement sur `127.0.0.1`.
- Les commandes `/dashboard on`, `/dashboard off` et `/dashboard status` démarrent, arrêtent et décrivent le service. Elles gèrent l’état courant ; le démarrage automatique se configure séparément dans `/config`.
- Le dashboard est servi par le binaire CLI avec ses ressources web intégrées. `on` affiche l’URL et n’ouvre pas automatiquement un navigateur.
- Les statistiques actives sont actualisées régulièrement et leur historique reste local pendant 90 jours par défaut. La durée est configurable.
- La page des conversations est désactivée par défaut et peut être activée ou désactivée dans `/config`. Ce réglage masque l’accès web aux conversations sans supprimer les archives.
- Les conversations archivées sont listées de la plus récente à la plus ancienne, peuvent être recherchées et consultées, et offrent la commande `/load <id>` à copier.
- L’analyse métrique par IA se lance sur demande. Elle reçoit seulement des métriques agrégées et les informations documentées du catalogue de modèles.
- Une analyse personnalisée peut aussi lire les conversations archivées. Cette option est désactivée par défaut, configurable indépendamment de leur affichage et ne fonctionne qu’après activation puis action explicite dans le dashboard.
- Le budget initial de cette analyse est de 8 000 tokens estimés pour la requête complète. Le plafond est configurable et le dashboard montre une estimation ainsi que le nombre de sessions incluses avant l’envoi.
- La rétention des archives de conversation est alignée sur celle des statistiques : 90 jours par défaut, configurable dans les réglages du dashboard.
- Le contenu des conversations et les messages envoyés pour une analyse ne sont jamais ajoutés aux journaux du serveur.

## Approche

Créer un petit serveur HTTP dédié au dashboard, dans le processus de la CLI, sur un port distinct (8765 par défaut). Les ressources HTML, CSS et JavaScript sont intégrées au binaire. Une implémentation avec la bibliothèque standard HTTP suffit : elle évite de lier le dashboard au cycle de vie ou aux réglages d’accès de l’API REST.

Le serveur reçoit des dépendances explicites : configuration, session CLI, gestionnaire d’historique et fournisseur d’analyses IA. L’interface web reste une cliente en lecture seule, à l’exception des requêtes explicites d’analyse IA. Elle n’utilise ni l’API REST publique de chat, ni le navigateur pour envoyer des commandes au processus CLI.

## Commandes et configuration

Ajouter `/dashboard` au registre central des commandes afin qu’il apparaisse dans l’autocomplétion, l’aide terminale et la documentation web. La syntaxe acceptée est :

```text
/dashboard on
/dashboard off
/dashboard status
```

Sans argument, la commande affiche l’usage. Les arguments inconnus sont rejetés avec cet usage.

Ajouter une section Dashboard à `/config` avec les réglages suivants et leurs valeurs initiales :

| Réglage | Valeur initiale | Effet |
| --- | --- | --- |
| Port local | `8765` | Port du serveur dédié ; l’adresse reste fixée à `127.0.0.1`. |
| Démarrage automatique | `false` | Démarre le service avec la CLI. |
| Fréquence d’actualisation | `3` secondes | Intervalle des mises à jour de l’interface. |
| Rétention | `90` jours | Durée commune aux archives de statistiques et de conversations. |
| Afficher les conversations | `false` | Autorise les pages et routes de consultation des conversations. |
| Autoriser l’analyse IA des conversations | `false` | Autorise le rapport personnalisé à lire les archives après action explicite. |
| Plafond IA | `8000` tokens estimés | Budget approximatif maximal pour une analyse personnalisée. |

Le réglage de démarrage automatique est indépendant de `/dashboard on` et `/dashboard off`. Un changement de port pendant que le serveur tourne s’applique au prochain démarrage. À l’arrêt normal de la CLI, le dashboard est arrêté.

## Statistiques et stockage

Les compteurs en mémoire sont issus de `internal/analytics.ChatAnalytics`. Ajouter un instantané concurrentiel sûr, sans exposer le mutex ou les champs mutables à l’interface. Ajouter les mesures par modèle nécessaires au dashboard : nombre d’interactions, réussites, échecs et temps de réponse agrégé. Ne pas déduire de coût ou de disponibilité réelle à partir du seul catalogue statique.

L’interface présente au minimum :

- activité de la session courante et tendance des sessions archivées ;
- volumes de messages, recherches, fichiers et URL ;
- temps de réponse, taux de réussite et erreurs ;
- optimisations de contexte et estimation de tokens ;
- utilisation observée de chaque modèle, avec sa latence et son taux de réussite ;
- commandes les plus utilisées.

Les tokens restent marqués comme estimations. Aucun coût n’est affiché tant que la CLI ne dispose pas d’une mesure fiable.

Écrire les agrégats de session dans un fichier de données distinct des conversations, sous le répertoire utilisateur de configuration de la CLI. La sauvegarde est atomique et limitée aux permissions de l’utilisateur. Elle est mise à jour périodiquement et à la fin d’une session, même si le serveur web n’est pas lancé, afin que l’historique soit disponible au prochain lancement. À la lecture, ignorer et signaler les entrées corrompues sans rendre le reste de l’historique inaccessible.

La rétention configurable s’applique aux deux magasins. Adapter la rétention du `HistoryManager` existant et exécuter le nettoyage au démarrage et lorsqu’une nouvelle valeur est enregistrée. Le plafond actuel de 100 sessions du gestionnaire d’historique demeure également applicable.

## Conversations

Réutiliser les archives compressées de `internal/persistence.HistoryManager`; ne pas créer un second magasin de transcriptions. La liste montre la date, le modèle, le nombre de messages, un aperçu de la première intervention utilisateur et l’identifiant de session. Elle est triée par date décroissante et propose une recherche locale. Une sélection ouvre les messages archivés dans l’ordre de la conversation. Un bouton copie `/load <id>`.

Les archives actuelles sont surtout écrites lors d’un effacement ou du chargement d’une autre session. Ajouter une sauvegarde finale synchrone lors d’une sortie normale (`/exit`, fin du prompt et interruption de fermeture) pour qu’une conversation terminée soit consultable. Éviter de dépendre d’une goroutine non attendue avant `os.Exit`. La session courante apparaît dans la liste après sa sauvegarde ; l’interface ne pilote pas le prompt CLI.

Quand « Afficher les conversations » est désactivé, masquer la page et refuser les routes de liste et de lecture. Les archives restent sur disque et continuent d’être utilisables par `/load`. L’option d’analyse conversationnelle est indépendante : elle autorise une lecture serveur des archives pour l’analyse, mais ne réactive pas les routes ou la page de consultation.

## Analyses IA et modèles

Fournir deux actions explicites dans le tableau de bord :

1. **Analyser les statistiques** : transmettre seulement un instantané des métriques agrégées et les informations de `models.Available()` au modèle choisi. L’action ne lit aucune conversation.
2. **Créer un rapport personnalisé** : disponible seulement si l’analyse des conversations est autorisée dans `/config`. Le tableau de bord indique que des extraits seront envoyés à Duck.ai, affiche le nombre de sessions concernées et l’estimation de tokens, puis attend le clic de l’utilisateur.

Le rapport personnalisé couvre les archives présentes dans la fenêtre de rétention. Construire un échantillon déterministe d’extraits répartis entre les sessions afin de représenter l’ensemble sans dépasser le plafond estimé. Calculer le budget à partir du texte réellement préparé, y compris les consignes d’analyse. Le plafond est approximatif (estimation locale d’environ quatre caractères par token) et doit être présenté comme tel. Limiter aussi la longueur demandée de la réponse et signaler clairement que le quota réel du fournisseur peut différer.

Le modèle est choisi dans le catalogue exposé par la CLI, avec le modèle courant présélectionné. Exécuter l’analyse via une requête isolée qui ne change pas le modèle, l’historique, les compteurs de conversation ou le contexte de la CLI. Basculer entre modèles selon les données de performance observées et les descriptions du catalogue est permis en recommandation, mais ne pas prétendre vérifier en temps réel qu’un modèle est disponible chez Duck.ai. Afficher séparément les observations et les recommandations générées.

Si Duck.ai est indisponible ou la requête échoue, afficher un message utile sans altérer les statistiques. Empêcher les requêtes identiques simultanées depuis l’interface.

## Pages et présentation

Créer une interface sombre, minimaliste, responsive et lisible, sans dépendance web installée séparément. Elle comprend :

- **Vue d’ensemble** : indicateurs, tendance temporelle, répartition des modèles et commandes, puis actions d’analyse IA ;
- **Sessions** : disponible seulement si l’affichage des conversations est activé ;
- **Commandes** : documentation web issue du registre central, organisée par catégories et présentant syntaxe et description des commandes ainsi que des exemples. Cette page inclut `/dashboard` et rappelle que les commandes se lancent dans le terminal.

Actualiser les statistiques selon la fréquence configurée. Afficher une heure de dernière actualisation et des états vides explicites au premier lancement.

Dans le terminal, définir des couleurs cohérentes à fort contraste pour le texte, l’arrière-plan, la description, l’élément sélectionné et la complétion anticipée du menu `go-prompt`. Revoir aussi la table d’aide pour conserver un contraste lisible sur le thème sombre.

## Routes et limites d’accès

Exposer des routes dédiées au dashboard, sans ajouter ces données à l’API REST générale :

- document HTML et ressources statiques ;
- instantané des statistiques courantes et agrégats historiques ;
- documentation des commandes ;
- liste et détail des conversations, seulement si l’option d’affichage est activée ;
- actions POST distinctes pour les deux analyses IA, avec refus explicite si l’option correspondante est désactivée.

Le serveur écoute exclusivement sur `127.0.0.1`, ne configure aucun CORS et valide l’en-tête `Host`. Les actions POST vérifient l’origine attendue. Ne jamais accepter un chemin de fichier fourni par le navigateur ; les archives sont adressées par identifiant de session validé.

## Cycle de vie et erreurs

- `on` démarre le serveur en arrière-plan et affiche l’URL ; si le port est occupé, la commande affiche l’erreur et la CLI continue.
- `off` arrête proprement le serveur et libère le port.
- `status` indique s’il fonctionne et son URL lorsqu’il est actif.
- `autostart` utilise les mêmes vérifications de port et signale les erreurs sans empêcher le démarrage de la CLI.
- Une session corrompue est omise de la liste et signalée ; les autres sessions restent disponibles.
- Une panne du navigateur ne touche pas la CLI ; une panne de Duck.ai ne touche pas les données locales.
- L’arrêt de la CLI finalise l’archive de conversation et les statistiques avant de fermer le serveur.

## Hors périmètre

- Exposer le serveur dashboard ou l’API sur le réseau local ou Internet.
- Envoyer des conversations à l’IA sans activation dans `/config` et clic explicite.
- Afficher des coûts ou des nombres exacts de tokens que le backend ne fournit pas.
- Déclencher les commandes CLI directement depuis la page web.
- Ajouter un système de comptes utilisateurs, une base de données serveur ou une dépendance JavaScript à installer.

## Critères d’acceptation

1. Les trois sous-commandes dashboard sont reconnues et l’état est exact ; le démarrage automatique et les réglages sont persistés.
2. La page web, servie depuis le binaire, reste inaccessible sur toute adresse non loopback.
3. Les indicateurs se mettent à jour sans recharger la page et l’historique agrégé respecte la rétention configurable de 90 jours par défaut.
4. Les mesures par modèle restent cohérentes et sûres lors d’un accès concurrent.
5. Les conversations sont cachées et leurs routes refusées par défaut ; une fois activées, les archives peuvent être cherchées, lues et reprises avec la commande copiée.
6. Les sessions sont enregistrées à la sortie normale sans course avec `os.Exit`.
7. L’analyse métrique n’envoie que des données agrégées ; l’analyse conversationnelle est indépendante, désactivée par défaut, demande un clic, montre son estimation et respecte un budget configurable initial de 8 000 tokens estimés.
8. Les analyses n’altèrent pas la conversation active ; les pannes externes n’endommagent pas l’historique local.
9. La page Commandes dérive du registre de la CLI et le menu de suggestions terminales reste lisible sur le thème sombre.
